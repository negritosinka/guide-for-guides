package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/auth"
	"guide-for-guides/internal/db"
	"guide-for-guides/migrations"
	"guide-for-guides/web"
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

	tmpl := template.Must(template.ParseFS(web.FS, "templates/*.html"))
	authH := &auth.Handler{Pool: pool, Tmpl: tmpl}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "db down: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("GET /register", authH.RegisterForm)
	mux.HandleFunc("POST /register", authH.RegisterSubmit)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello! Guide for Guides is running. Go to /register")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
