package auth

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler хранит всё, что нужно обработчикам
type Handler struct {
	Pool    *pgxpool.Pool
	Tmpl    *template.Template
	BaseURL string // адрес сайта, нужен для ссылок в письмах
}

// PageData для страницы регистрации
type PageData struct {
	Error string
	Done  bool
	Email string
}

// MessageData для простой страницы с сообщением
type MessageData struct {
	Title string
	Text  string
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

	// 3. Сохраняем в базу. RETURNING id возвращает номер нового пользователя.
	// Если email уже есть, строка не создаётся и мы получаем ErrNoRows.
	var userID int64
	err = h.Pool.QueryRow(r.Context(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING RETURNING id`,
		email, hash).Scan(&userID)

	if err == nil {
		// Новый пользователь: создаём письмо с токеном
		h.sendVerifyEmail(r.Context(), userID, email)
	} else if err != pgx.ErrNoRows {
		// Настоящая ошибка базы
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	// Если err == pgx.ErrNoRows, такой email уже есть. Ничего не делаем.

	// 4. Ответ одинаковый в обоих случаях
	h.show(w, http.StatusOK, PageData{Done: true})
}

// sendVerifyEmail создаёт токен и "отправляет" письмо
func (h *Handler) sendVerifyEmail(ctx context.Context, userID int64, email string) {
	token, err := NewToken()
	if err != nil {
		log.Println("ошибка токена:", err)
		return
	}

	// В базу кладём только хеш токена, срок жизни 24 часа
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO email_tokens (user_id, token_hash, kind, expires_at)
		 VALUES ($1, $2, 'verify', now() + interval '24 hours')`,
		userID, HashToken(token))
	if err != nil {
		log.Println("ошибка сохранения токена:", err)
		return
	}

	// Пока почтового сервиса нет, печатаем ссылку в лог
	link := h.BaseURL + "/verify?token=" + token
	log.Println("VERIFY LINK для", email, ":", link)
}

// Verify вызывается, когда пользователь открыл ссылку из письма
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")

	// Ищем подходящий токен и сразу помечаем его использованным.
	// Условия: тип verify, ещё не использован, не просрочен.
	var userID int64
	err := h.Pool.QueryRow(r.Context(),
		`UPDATE email_tokens SET used_at = now()
		 WHERE token_hash = $1 AND kind = 'verify'
		   AND used_at IS NULL AND expires_at > now()
		 RETURNING user_id`,
		HashToken(token)).Scan(&userID)

	if err == pgx.ErrNoRows {
		h.showMessage(w, http.StatusBadRequest,
			"Kļūda", "Saite nav derīga vai ir novecojusi.")
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// Ставим пользователю дату подтверждения
	_, err = h.Pool.Exec(r.Context(),
		`UPDATE users SET email_verified_at = now() WHERE id = $1`, userID)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.showMessage(w, http.StatusOK,
		"E-pasts apstiprināts", "Paldies! Jūsu e-pasta adrese ir apstiprināta.")
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

// show выводит страницу регистрации
func (h *Handler) show(w http.ResponseWriter, status int, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := h.Tmpl.ExecuteTemplate(w, "register.html", data)
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}

// showMessage выводит страницу с заголовком и текстом
func (h *Handler) showMessage(w http.ResponseWriter, status int, title string, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := h.Tmpl.ExecuteTemplate(w, "message.html", MessageData{Title: title, Text: text})
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}
