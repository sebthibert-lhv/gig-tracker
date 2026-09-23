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

	mux.HandleFunc("POST /gigs", handleCreateGig(db))
	mux.HandleFunc("GET /gigs", handleListGigs(db))
	mux.HandleFunc("GET /gigs/{id}", handleGetGig(db))

	mux.HandleFunc("POST /gigs/{id}/photo", handleUploadGigPhoto(db))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	log.Println("starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}