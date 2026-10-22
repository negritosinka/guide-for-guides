package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/db"
	"guide-for-guides/migrations"
)

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "db down: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello! Guide for Guides is running. Check /health for the database.")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
