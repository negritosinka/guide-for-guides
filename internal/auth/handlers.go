package auth

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	Pool *pgxpool.Pool
	Tmpl *template.Template
}

type pageData struct {
	Error string
	Done  bool
	Email string
}

func (h *Handler) RegisterForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, pageData{})
}

func (h *Handler) RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	if msg := validate(email, password); msg != "" {
		h.render(w, http.StatusBadRequest, pageData{Error: msg, Email: email})
		return
	}

	hash, err := HashPassword(password)
	if err != nil {
		log.Println("hash error:", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Если такой email уже есть, вставка молча ничего не делает.
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		email, hash)
	if err != nil {
		log.Println("insert user error:", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Ответ одинаковый и для нового, и для уже существующего email,
	// чтобы нельзя было узнать, кто зарегистрирован.
	h.render(w, http.StatusOK, pageData{Done: true})
}

func validate(email, password string) string {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return "Введите корректный email."
	}
	if len(password) < 10 {
		return "Пароль должен быть не короче 10 символов."
	}
	if len(password) > 128 {
		return "Пароль слишком длинный (максимум 128 символов)."
	}
	return ""
}

func (h *Handler) render(w http.ResponseWriter, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.Tmpl.ExecuteTemplate(w, "register.html", data); err != nil {
		log.Println("template error:", err)
	}
}
