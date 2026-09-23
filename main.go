package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	db := connectDB()
	defer db.Close()

	rdb := connectRedis()
	defer rdb.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handleHealth)

	mux.HandleFunc("POST /venues", handleCreateVenue(db))
	mux.HandleFunc("GET /venues", handleListVenues(db))
	mux.HandleFunc("GET /venues/{id}", handleGetVenue(db))

	mux.HandleFunc("POST /bands", handleCreateBand(db))
	mux.HandleFunc("GET /bands", handleListBands(db))
	mux.HandleFunc("GET /bands/{id}", handleGetBand(db))

	mux.HandleFunc("POST /gigs", handleCreateGig(db, rdb))
	mux.HandleFunc("GET /gigs", handleListGigs(db, rdb))
	mux.HandleFunc("GET /gigs/{id}", handleGetGig(db))

	mux.HandleFunc("POST /gigs/{id}/photo", handleUploadGigPhoto(db, rdb))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("starting server on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}