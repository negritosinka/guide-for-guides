package auth

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ForgotData для страницы "забыл пароль"
type ForgotData struct {
	Done bool
}

// ResetData для страницы с новым паролем
type ResetData struct {
	Token string
	Error string
}

// ForgotForm показывает форму с полем email
func (h *Handler) ForgotForm(w http.ResponseWriter, r *http.Request) {
	h.page(w, http.StatusOK, "forgot.html", ForgotData{})
}

// ForgotSubmit создаёт токен для сброса пароля
func (h *Handler) ForgotSubmit(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))

	// Ищем подтверждённый аккаунт с таким email
	var userID int64
	err = h.Pool.QueryRow(r.Context(),
		`SELECT id FROM users
		 WHERE lower(email) = lower($1) AND email_verified_at IS NOT NULL`,
		email).Scan(&userID)

	if err == nil {
		h.sendResetEmail(r.Context(), userID, email)
	} else if err != pgx.ErrNoRows {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// Ответ одинаковый, нашли мы аккаунт или нет
	h.page(w, http.StatusOK, "forgot.html", ForgotData{Done: true})
}

// sendResetEmail создаёт токен и "отправляет" письмо (пока печатает в лог)
func (h *Handler) sendResetEmail(ctx context.Context, userID int64, email string) {
	token, err := NewToken()
	if err != nil {
		log.Println("ошибка токена:", err)
		return
	}

	_, err = h.Pool.Exec(ctx,
		`INSERT INTO email_tokens (user_id, token_hash, kind, expires_at)
		 VALUES ($1, $2, 'reset', now() + interval '1 hour')`,
		userID, HashToken(token))
	if err != nil {
		log.Println("ошибка сохранения токена:", err)
		return
	}

	log.Println("RESET LINK для", email, ":", h.BaseURL+"/reset?token="+token)
}

// ResetForm показывает форму нового пароля
func (h *Handler) ResetForm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	h.page(w, http.StatusOK, "reset.html", ResetData{Token: token})
}

// ResetSubmit меняет пароль
func (h *Handler) ResetSubmit(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	password := r.FormValue("password")

	// 1. Проверяем длину нового пароля
	if len(password) < 10 || len(password) > 128 {
		h.page(w, http.StatusBadRequest, "reset.html", ResetData{
			Token: token,
			Error: "Parolei jābūt no 10 līdz 128 rakstzīmēm.",
		})
		return
	}

	// 2. Хешируем новый пароль
	hash, err := HashPassword(password)
	if err != nil {
		log.Println("ошибка хеша:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 3. Проверяем токен и сразу помечаем его использованным
	var userID int64
	err = h.Pool.QueryRow(r.Context(),
		`UPDATE email_tokens SET used_at = now()
		 WHERE token_hash = $1 AND kind = 'reset'
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

	// 4. Сохраняем новый хеш пароля
	_, err = h.Pool.Exec(r.Context(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, userID)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 5. Удаляем все сессии: пользователя выкинет со всех устройств
	h.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE user_id = $1`, userID)

	h.showMessage(w, http.StatusOK,
		"Parole nomainīta", "Jūsu parole ir nomainīta. Tagad varat ieiet ar jauno paroli.")
}

// page показывает любую страницу по имени шаблона
func (h *Handler) page(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := h.Tmpl.ExecuteTemplate(w, name, data)
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}
