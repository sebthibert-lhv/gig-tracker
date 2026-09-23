package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

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
		var g Gig
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if len(g.Bands) == 0 {
			http.Error(w, "a gig must have at least one band", http.StatusBadRequest)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback() // no-op if Commit() already succeeded

		insertGig := `INSERT INTO gigs (venue_id, date, notes) VALUES ($1, $2, $3) RETURNING id`
		if err := tx.QueryRow(insertGig, g.VenueID, g.Date, g.Notes).Scan(&g.ID); err != nil {
			http.Error(w, "failed to create gig", http.StatusInternalServerError)
			return
		}

		insertGigBand := `INSERT INTO gig_bands (gig_id, band_id, is_headliner) VALUES ($1, $2, $3)`
		for _, gb := range g.Bands {
			if _, err := tx.Exec(insertGigBand, g.ID, gb.BandID, gb.IsHeadliner); err != nil {
				http.Error(w, "failed to attach band to gig", http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(); err != nil {
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

		// 10 MB max upload size
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

		ext := filepath.Ext(header.Filename)
		filename := fmt.Sprintf("%d%s", id, ext)
		destPath := filepath.Join("uploads", filename)

		if err := os.MkdirAll("uploads", 0755); err != nil {
			http.Error(w, "failed to prepare storage", http.StatusInternalServerError)
			return
		}

		dst, err := os.Create(destPath)
		if err != nil {
			http.Error(w, "failed to save file", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			http.Error(w, "failed to write file", http.StatusInternalServerError)
			return
		}

		photoURL := "/" + destPath
		_, err = db.Exec(`UPDATE gigs SET photo_url = $1 WHERE id = $2`, photoURL, id)
		if err != nil {
			http.Error(w, "failed to update gig", http.StatusInternalServerError)
			return
		}

		if err := rdb.Del(r.Context(), gigsListCacheKey).Err(); err != nil {
			log.Printf("redis del error: %v", err)
		}

		writeJSON(w, http.StatusOK, map[string]string{"photo_url": photoURL})
	}
}