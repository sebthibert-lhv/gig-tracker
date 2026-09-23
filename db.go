package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func connectDB() *sql.DB {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://giguser:gigpass@localhost:5432/gigtracker?sslmode=disable"
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("failed to open db connection: %v", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping db: %v", err)
	}

	return db
}