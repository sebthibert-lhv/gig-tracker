package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

type Venue struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	City string `json:"city"`
}

func handleCreateVenue(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var v Venue
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		query := `INSERT INTO venues (name, city) VALUES ($1, $2) RETURNING id`
		err := db.QueryRow(query, v.Name, v.City).Scan(&v.ID)
		if err != nil {
			http.Error(w, "failed to create venue", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, v)
	}
}

func handleListVenues(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, name, city FROM venues ORDER BY id`)
		if err != nil {
			http.Error(w, "failed to fetch venues", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		venues := []Venue{}
		for rows.Next() {
			var v Venue
			if err := rows.Scan(&v.ID, &v.Name, &v.City); err != nil {
				http.Error(w, "failed to read venue row", http.StatusInternalServerError)
				return
			}
			venues = append(venues, v)
		}

		writeJSON(w, http.StatusOK, venues)
	}
}

func handleGetVenue(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid venue id", http.StatusBadRequest)
			return
		}

		var v Venue
		query := `SELECT id, name, city FROM venues WHERE id = $1`
		err = db.QueryRow(query, id).Scan(&v.ID, &v.Name, &v.City)
		if err == sql.ErrNoRows {
			http.Error(w, "venue not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "failed to fetch venue", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, v)
	}
}