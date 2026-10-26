package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/auth"
	"guide-for-guides/internal/db"
	"guide-for-guides/migrations"
	"guide-for-guides/web"
)

func main() {
	ctx := context.Background()

	// Подключаемся к базе (адрес берём из docker-compose.yml)
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	// Создаём таблицы, если их ещё нет
	err = db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		log.Fatal(err)
	}

	// Загружаем HTML-шаблоны
	tmpl := template.Must(template.ParseFS(web.FS, "templates/*.html"))

	// Адрес сайта для ссылок в письмах. Локально это localhost.
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	authHandler := &auth.Handler{Pool: pool, Tmpl: tmpl, BaseURL: baseURL}

	// Адреса сайта: какой адрес какая функция обрабатывает
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		err := pool.Ping(r.Context())
		if err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("GET /register", authHandler.RegisterForm)
	mux.HandleFunc("GET /verify", authHandler.Verify)
	mux.HandleFunc("POST /register", authHandler.RegisterSubmit)

	mux.HandleFunc("GET /login", authHandler.LoginForm)
	mux.HandleFunc("POST /login", authHandler.LoginSubmit)
	mux.HandleFunc("GET /account", authHandler.Account)
	mux.HandleFunc("POST /logout", authHandler.Logout)

	mux.HandleFunc("GET /forgot", authHandler.ForgotForm)
	mux.HandleFunc("POST /forgot", authHandler.ForgotSubmit)
	mux.HandleFunc("GET /reset", authHandler.ResetForm)
	mux.HandleFunc("POST /reset", authHandler.ResetSubmit)

	// {$} значит "только главная страница"
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Sveiki! Guide for Guides darbojas. Dodieties uz /register vai /login")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
