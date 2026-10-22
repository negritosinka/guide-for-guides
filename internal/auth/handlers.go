package auth

import (
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler хранит всё, что нужно обработчикам: базу и шаблоны
type Handler struct {
	Pool *pgxpool.Pool
	Tmpl *template.Template
}

// PageData это данные, которые мы передаём в HTML-шаблон
type PageData struct {
	Error string // текст ошибки
	Done  bool   // true, если регистрация прошла
	Email string // чтобы не стирать email в форме после ошибки
}

// RegisterForm показывает пустую форму
func (h *Handler) RegisterForm(w http.ResponseWriter, r *http.Request) {
	h.show(w, http.StatusOK, PageData{})
}

// RegisterSubmit вызывается, когда пользователь нажал кнопку
func (h *Handler) RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	// 1. Проверяем данные
	errText := checkInput(email, password)
	if errText != "" {
		h.show(w, http.StatusBadRequest, PageData{Error: errText, Email: email})
		return
	}

	// 2. Делаем хеш пароля
	hash, err := HashPassword(password)
	if err != nil {
		log.Println("ошибка хеша:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 3. Сохраняем в базу. Если такой email уже есть, ничего не происходит.
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		email, hash)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 4. Ответ одинаковый для нового и для существующего email,
	// чтобы нельзя было узнать, кто уже зарегистрирован
	h.show(w, http.StatusOK, PageData{Done: true})
}

// checkInput возвращает текст ошибки или пустую строку, если всё хорошо
func checkInput(email string, password string) string {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return "Ievadiet derīgu e-pasta adresi."
	}
	if len(password) < 10 {
		return "Parolei jābūt vismaz 10 rakstzīmes garai."
	}
	if len(password) > 128 {
		return "Parole ir pārāk gara (maksimums 128 rakstzīmes)."
	}
	return ""
}

// show выводит HTML-страницу из шаблона
func (h *Handler) show(w http.ResponseWriter, status int, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := h.Tmpl.ExecuteTemplate(w, "register.html", data)
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}
