package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

type Band struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func handleCreateBand(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b Band
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		query := `INSERT INTO bands (name) VALUES ($1) RETURNING id`
		err := db.QueryRow(query, b.Name).Scan(&b.ID)
		if err != nil {
			http.Error(w, "failed to create band", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, b)
	}
}

func handleListBands(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, name FROM bands ORDER BY id`)
		if err != nil {
			http.Error(w, "failed to fetch bands", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		bands := []Band{}
		for rows.Next() {
			var b Band
			if err := rows.Scan(&b.ID, &b.Name); err != nil {
				http.Error(w, "failed to read band row", http.StatusInternalServerError)
				return
			}
			bands = append(bands, b)
		}

		writeJSON(w, http.StatusOK, bands)
	}
}

func handleGetBand(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid band id", http.StatusBadRequest)
			return
		}

		var b Band
		query := `SELECT id, name FROM bands WHERE id = $1`
		err = db.QueryRow(query, id).Scan(&b.ID, &b.Name)
		if err == sql.ErrNoRows {
			http.Error(w, "band not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "failed to fetch band", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, b)
	}
}