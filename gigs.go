package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type CreateGigBand struct {
	Name        string `json:"name"`
	IsHeadliner bool   `json:"is_headliner"`
}

type CreateGigVenue struct {
	Name string `json:"name"`
	City string `json:"city"`
}

type CreateGigRequest struct {
	Venue CreateGigVenue  `json:"venue"`
	Date  string          `json:"date"`
	Notes string          `json:"notes"`
	Bands []CreateGigBand `json:"bands"`
}

type GigBand struct {
	BandID      int    `json:"band_id"`
	BandName    string `json:"band_name,omitempty"`
	IsHeadliner bool   `json:"is_headliner"`
}

type Gig struct {
	ID       int       `json:"id"`
	VenueID  int       `json:"venue_id"`
	Date     string    `json:"date"`
	Notes    string    `json:"notes"`
	PhotoURL string    `json:"photo_url"`
	Bands    []GigBand `json:"bands"`
}

const gigsListCacheKey = "gigs:list"

func handleCreateGig(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "expected a multipart form under 10MB", http.StatusBadRequest)
			return
		}

		gigJSON := r.FormValue("gig")
		if gigJSON == "" {
			http.Error(w, "missing gig field", http.StatusBadRequest)
			return
		}

		var req CreateGigRequest
		if err := json.Unmarshal([]byte(gigJSON), &req); err != nil {
			http.Error(w, "gig field is not valid JSON", http.StatusBadRequest)
			return
		}

		// photo is optional
		file, header, err := r.FormFile("photo")
		hasPhoto := err == nil
		if err != nil && err != http.ErrMissingFile {
			http.Error(w, "invalid photo", http.StatusBadRequest)
			return
		}
		if hasPhoto {
			defer file.Close()
		}

		// --- unchanged from here: validation and band cleanup ---
		req.Venue.Name = strings.TrimSpace(req.Venue.Name)
		req.Venue.City = strings.TrimSpace(req.Venue.City)
		if req.Venue.Name == "" {
			http.Error(w, "venue name is required", http.StatusBadRequest)
			return
		}
		if req.Date == "" {
			http.Error(w, "date is required", http.StatusBadRequest)
			return
		}

		seen := map[string]bool{}
		var bands []CreateGigBand
		for _, b := range req.Bands {
			name := strings.TrimSpace(b.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			bands = append(bands, CreateGigBand{Name: name, IsHeadliner: b.IsHeadliner})
		}
		if len(bands) == 0 {
			http.Error(w, "a gig must have at least one band", http.StatusBadRequest)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		venueID, err := findOrCreateVenue(tx, req.Venue.Name, req.Venue.City)
		if err != nil {
			log.Printf("find or create venue: %v", err)
			http.Error(w, "failed to save venue", http.StatusInternalServerError)
			return
		}

		g := Gig{VenueID: venueID, Date: req.Date, Notes: req.Notes, Bands: []GigBand{}}

		insertGig := `INSERT INTO gigs (venue_id, date, notes) VALUES ($1, $2, $3) RETURNING id`
		if err := tx.QueryRow(insertGig, venueID, req.Date, req.Notes).Scan(&g.ID); err != nil {
			log.Printf("insert gig: %v", err)
			http.Error(w, "failed to create gig", http.StatusInternalServerError)
			return
		}

		insertGigBand := `INSERT INTO gig_bands (gig_id, band_id, is_headliner) VALUES ($1, $2, $3)`
		for _, b := range bands {
			bandID, storedName, err := findOrCreateBand(tx, b.Name)
			if err != nil {
				log.Printf("find or create band %q: %v", b.Name, err)
				http.Error(w, "failed to save band", http.StatusInternalServerError)
				return
			}
			if _, err := tx.Exec(insertGigBand, g.ID, bandID, b.IsHeadliner); err != nil {
				log.Printf("attach band to gig: %v", err)
				http.Error(w, "failed to attach band to gig", http.StatusInternalServerError)
				return
			}
			g.Bands = append(g.Bands, GigBand{BandID: bandID, BandName: storedName, IsHeadliner: b.IsHeadliner})
		}
		// --- end unchanged ---

		// photo goes last: everything that can fail in the database has already succeeded
		var savedPath string
		if hasPhoto {
			photoURL, destPath, err := savePhoto(file, header, g.ID)
			if err != nil {
				log.Printf("save photo: %v", err)
				http.Error(w, "failed to save photo", http.StatusInternalServerError)
				return
			}
			savedPath = destPath

			if _, err := tx.Exec(`UPDATE gigs SET photo_url = $1 WHERE id = $2`, photoURL, g.ID); err != nil {
				os.Remove(savedPath)
				log.Printf("set photo url: %v", err)
				http.Error(w, "failed to attach photo", http.StatusInternalServerError)
				return
			}
			g.PhotoURL = photoURL
		}

		if err := tx.Commit(); err != nil {
			if savedPath != "" {
				os.Remove(savedPath)
			}
			log.Printf("commit: %v", err)
			http.Error(w, "failed to commit transaction", http.StatusInternalServerError)
			return
		}

		if err := rdb.Del(r.Context(), gigsListCacheKey).Err(); err != nil {
			log.Printf("redis del error: %v", err)
		}

		writeJSON(w, http.StatusCreated, g)
	}
}

func handleListGigs(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		cached, err := rdb.Get(ctx, gigsListCacheKey).Result()
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.Write([]byte(cached))
			return
		}
		if err != redis.Nil {
			log.Printf("redis get error: %v", err)
		}

		gigRows, err := db.Query(`SELECT id, venue_id, date, notes, photo_url FROM gigs ORDER BY date DESC`)
		if err != nil {
			http.Error(w, "failed to fetch gigs", http.StatusInternalServerError)
			return
		}
		defer gigRows.Close()

		gigsByID := map[int]*Gig{}
		var order []int
		for gigRows.Next() {
			g := &Gig{Bands: []GigBand{}}
			var notes, photoURL sql.NullString
			if err := gigRows.Scan(&g.ID, &g.VenueID, &g.Date, &notes, &photoURL); err != nil {
				http.Error(w, "failed to read gig row", http.StatusInternalServerError)
				return
			}
			g.Notes = notes.String
			g.PhotoURL = photoURL.String
			gigsByID[g.ID] = g
			order = append(order, g.ID)
		}

		bandRows, err := db.Query(`
			SELECT gb.gig_id, b.id, b.name, gb.is_headliner
			FROM gig_bands gb
			JOIN bands b ON b.id = gb.band_id
		`)
		if err != nil {
			http.Error(w, "failed to fetch gig bands", http.StatusInternalServerError)
			return
		}
		defer bandRows.Close()

		for bandRows.Next() {
			var gigID int
			var gb GigBand
			if err := bandRows.Scan(&gigID, &gb.BandID, &gb.BandName, &gb.IsHeadliner); err != nil {
				http.Error(w, "failed to read gig_band row", http.StatusInternalServerError)
				return
			}
			if g, ok := gigsByID[gigID]; ok {
				g.Bands = append(g.Bands, gb)
			}
		}

		gigs := make([]*Gig, 0, len(order))
		for _, id := range order {
			gigs = append(gigs, gigsByID[id])
		}


		payload, err := json.Marshal(gigs)
		if err != nil {
			http.Error(w, "failed to serialize gigs", http.StatusInternalServerError)
			return
		}

		if err := rdb.Set(ctx, gigsListCacheKey, payload, 5*time.Minute).Err(); err != nil {
			log.Printf("redis set error: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		w.Write(payload)
	}
}

func handleGetGig(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid gig id", http.StatusBadRequest)
			return
		}

		g := &Gig{Bands: []GigBand{}}
		var notes, photoURL sql.NullString

		query := `SELECT id, venue_id, date, notes, photo_url FROM gigs WHERE id = $1`
		err = db.QueryRow(query, id).Scan(&g.ID, &g.VenueID, &g.Date, &notes, &photoURL)
		if err == sql.ErrNoRows {
			http.Error(w, "gig not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "failed to fetch gig", http.StatusInternalServerError)
			return
		}
		g.Notes = notes.String
		g.PhotoURL = photoURL.String

		bandRows, err := db.Query(`
			SELECT b.id, b.name, gb.is_headliner
			FROM gig_bands gb
			JOIN bands b ON b.id = gb.band_id
			WHERE gb.gig_id = $1
		`, g.ID)
		if err != nil {
			http.Error(w, "failed to fetch gig bands", http.StatusInternalServerError)
			return
		}
		defer bandRows.Close()

		for bandRows.Next() {
			var gb GigBand
			if err := bandRows.Scan(&gb.BandID, &gb.BandName, &gb.IsHeadliner); err != nil {
				http.Error(w, "failed to read gig_band row", http.StatusInternalServerError)
				return
			}
			g.Bands = append(g.Bands, gb)
		}

		writeJSON(w, http.StatusOK, g)
	}
}

func handleUploadGigPhoto(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid gig id", http.StatusBadRequest)
			return
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "file too large or invalid form", http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("photo")
		if err != nil {
			http.Error(w, "missing photo field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		photoURL, destPath, err := savePhoto(file, header, id)
		if err != nil {
			log.Printf("save photo: %v", err)
			http.Error(w, "failed to save photo", http.StatusInternalServerError)
			return
		}

		result, err := db.Exec(`UPDATE gigs SET photo_url = $1 WHERE id = $2`, photoURL, id)
		if err != nil {
			os.Remove(destPath)
			log.Printf("update gig photo: %v", err)
			http.Error(w, "failed to update gig", http.StatusInternalServerError)
			return
		}
		if rows, _ := result.RowsAffected(); rows == 0 {
			os.Remove(destPath)
			http.Error(w, "gig not found", http.StatusNotFound)
			return
		}

		if err := rdb.Del(r.Context(), gigsListCacheKey).Err(); err != nil {
			log.Printf("redis del error: %v", err)
		}

		writeJSON(w, http.StatusOK, map[string]string{"photo_url": photoURL})
	}
}

// savePhoto writes an uploaded photo to disk, named after the gig.
// It returns the public URL and the file path on disk (needed for cleanup).
func savePhoto(file multipart.File, header *multipart.FileHeader, gigID int) (string, string, error) {
	if err := os.MkdirAll("uploads", 0755); err != nil {
		return "", "", fmt.Errorf("create uploads dir: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	destPath := filepath.Join("uploads", fmt.Sprintf("%d%s", gigID, ext))

	dst, err := os.Create(destPath)
	if err != nil {
		return "", "", fmt.Errorf("create file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(destPath)
		return "", "", fmt.Errorf("write file: %w", err)
	}

	return "/" + destPath, destPath, nil
}