package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	db := connectDB()
	defer db.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /venues", handleCreateVenue(db))
	mux.HandleFunc("GET /venues", handleListVenues(db))
	mux.HandleFunc("GET /venues/{id}", handleGetVenue(db))

	mux.HandleFunc("POST /bands", handleCreateBand(db))
	mux.HandleFunc("GET /bands", handleListBands(db))
	mux.HandleFunc("GET /bands/{id}", handleGetBand(db))

	mux.HandleFunc("GET /gigs", handleListGigs)

	log.Println("starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

func handleListGigs(w http.ResponseWriter, r *http.Request) {
	// placeholder data until the database is wired up in milestone 2
	gigs := []map[string]string{
		{"band": "Radiohead", "venue": "Alexandra Palace", "date": "2024-06-01"},
	}
	writeJSON(w, http.StatusOK, gigs)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}