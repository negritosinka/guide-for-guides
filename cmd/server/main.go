package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/admin"
	"guide-for-guides/internal/auth"
	"guide-for-guides/internal/boards"
	"guide-for-guides/internal/db"
	"guide-for-guides/internal/moderation"
	"guide-for-guides/internal/scanner"
	"guide-for-guides/migrations"
	"guide-for-guides/web"
	"strconv"
	"time"
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
	// Заносим доски из списка в базу
	err = boards.Sync(ctx, pool)
	if err != nil {
		log.Fatal(err)
	}

	// Запускаем сканер в фоне: сразу и потом каждые 6 часов
	parsers := []scanner.Parser{
		scanner.DemoParser{},
		scanner.FreeCodeCampParser{},
	}

	go scanner.Schedule(ctx, pool, parsers, 6*time.Hour)

	// Загружаем HTML-шаблоны
	tmpl := template.Must(template.ParseFS(web.FS, "templates/*.html"))

	// Адрес сайта для ссылок в письмах. Локально это localhost.
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	authHandler := &auth.Handler{Pool: pool, Tmpl: tmpl, BaseURL: baseURL}
	boardsHandler := &boards.Handler{Pool: pool, Tmpl: tmpl, Auth: authHandler}

	// Сколько флагов приводит к блокировке (по умолчанию 3)
	maxFlags := 3
	value, err := strconv.Atoi(os.Getenv("MAX_FLAGS"))
	if err == nil && value > 0 {
		maxFlags = value
	}

	adminHandler := &admin.Handler{Pool: pool, Tmpl: tmpl, Auth: authHandler, MaxFlags: maxFlags}
	moderationHandler := &moderation.Handler{Pool: pool, Tmpl: tmpl, Auth: authHandler}

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

	mux.HandleFunc("GET /admin", adminHandler.Dashboard)
	mux.HandleFunc("GET /admin/users", adminHandler.Users)
	mux.HandleFunc("POST /admin/users/role", adminHandler.ChangeRole)
	mux.HandleFunc("POST /admin/users/ban", adminHandler.ChangeBan)
	mux.HandleFunc("GET /admin/log", adminHandler.Log)

	mux.HandleFunc("GET /boards", boardsHandler.List)
	mux.HandleFunc("GET /boards/{slug}", boardsHandler.Show)
	mux.HandleFunc("GET /admin/boards", adminHandler.Boards)
	mux.HandleFunc("POST /admin/boards/update", adminHandler.UpdateBoard)

	mux.HandleFunc("GET /search", boardsHandler.Search)

	mux.HandleFunc("GET /tutorials/{id}", boardsHandler.Tutorial)
	mux.HandleFunc("POST /tutorials/{id}/bookmark", boardsHandler.Bookmark)
	mux.HandleFunc("POST /tutorials/{id}/review", boardsHandler.Review)
	mux.HandleFunc("POST /tutorials/{id}/review/delete", boardsHandler.DeleteReview)
	mux.HandleFunc("GET /bookmarks", boardsHandler.Bookmarks)

	mux.HandleFunc("GET /reviews/{id}/report", moderationHandler.ReportForm)
	mux.HandleFunc("POST /reviews/{id}/report", moderationHandler.ReportSubmit)
	mux.HandleFunc("GET /bug", moderationHandler.BugForm)
	mux.HandleFunc("POST /bug", moderationHandler.BugSubmit)
	mux.HandleFunc("GET /my/removed", moderationHandler.Removed)
	mux.HandleFunc("POST /appeals/review", moderationHandler.AppealReview)
	mux.HandleFunc("GET /appeal", moderationHandler.BanAppealForm)
	mux.HandleFunc("POST /appeal", moderationHandler.BanAppealSubmit)

	mux.HandleFunc("GET /admin/reports", adminHandler.Reports)
	mux.HandleFunc("POST /admin/reports/{id}/resolve", adminHandler.ResolveReport)
	mux.HandleFunc("GET /admin/appeals", adminHandler.Appeals)
	mux.HandleFunc("POST /admin/appeals/{id}/resolve", adminHandler.ResolveAppeal)

	mux.HandleFunc("GET /dmca", moderationHandler.DMCAForm)
	mux.HandleFunc("POST /dmca", moderationHandler.DMCASubmit)
	mux.HandleFunc("GET /tutorials/{id}/report", moderationHandler.TutorialReportForm)
	mux.HandleFunc("POST /tutorials/{id}/report", moderationHandler.TutorialReportSubmit)

	mux.HandleFunc("GET /admin/dmca", adminHandler.DMCAList)
	mux.HandleFunc("POST /admin/dmca/{id}/resolve", adminHandler.DMCAResolve)
	mux.HandleFunc("GET /admin/hidden", adminHandler.HiddenList)
	mux.HandleFunc("POST /admin/hidden/{id}/restore", adminHandler.Restore)

	// {$} значит "только главная страница"
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Sveiki! Guide for Guides darbojas. Dodieties uz /search, /boards, /register vai /login")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
