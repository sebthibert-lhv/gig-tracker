package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func handleUploadGigPhoto(db *sql.DB) http.HandlerFunc {
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

		writeJSON(w, http.StatusOK, map[string]string{"photo_url": photoURL})
	}
}