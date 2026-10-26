package auth

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// LoginData для страницы входа
type LoginData struct {
	Error string
	Email string
}

// AccountData для страницы аккаунта
type AccountData struct {
	Email string
}

// Одно и то же сообщение для любой причины неудачи.
// Так нельзя узнать, есть ли такой email в базе.
const loginError = "Nepareizs e-pasts vai parole."

// LoginForm показывает форму входа
func (h *Handler) LoginForm(w http.ResponseWriter, r *http.Request) {
	h.showLogin(w, http.StatusOK, LoginData{})
}

// LoginSubmit вызывается, когда пользователь нажал "Ieiet"
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	// Слишком много неудачных попыток? Тогда пароль даже не проверяем.
	if h.tooManyAttempts(r, email) {
		h.showLogin(w, http.StatusTooManyRequests, LoginData{
			Error: "Pārāk daudz mēģinājumu. Mēģiniet vēlreiz pēc 15 minūtēm.",
			Email: email,
		})
		return
	}

	// 1. Ищем пользователя по email (без учёта регистра букв)
	var userID int64
	var hash string
	var verifiedAt *time.Time // указатель, потому что в базе может быть NULL
	var bannedAt *time.Time

	err = h.Pool.QueryRow(r.Context(),
		`SELECT id, password_hash, email_verified_at, banned_at
		 FROM users WHERE lower(email) = lower($1)`,
		email).Scan(&userID, &hash, &verifiedAt, &bannedAt)

	if err == pgx.ErrNoRows {
		// Пользователя нет. Всё равно считаем хеш, чтобы ответ шёл
		// столько же времени, как и для настоящего пользователя.
		HashPassword("neeksistējošs-lietotājs")
		h.loginFailed(w, r, email)
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 2. Проверяем пароль
	if !CheckPassword(password, hash) {
		h.loginFailed(w, r, email)
		return
	}

	// Пароль верный: стираем неудачные попытки этого email
	h.Pool.Exec(r.Context(),
		`DELETE FROM login_attempts WHERE email_hash = $1`,
		HashToken(strings.ToLower(email)))

	// 3. Пароль верный. Теперь можно честно сказать, что не так с аккаунтом.
	if bannedAt != nil {
		h.showMessage(w, http.StatusForbidden,
			"Konts bloķēts", "Šis konts ir bloķēts.")
		return
	}
	if verifiedAt == nil {
		h.showMessage(w, http.StatusForbidden,
			"E-pasts nav apstiprināts", "Lūdzu, apstipriniet e-pastu, izmantojot saiti no vēstules.")
		return
	}

	// 4. Всё хорошо, создаём сессию
	token, err := NewToken()
	if err != nil {
		log.Println("ошибка токена:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	expires := time.Now().Add(7 * 24 * time.Hour) // сессия живёт 7 дней

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		HashToken(token), userID, expires)
	if err != nil {
		log.Println("ошибка сессии:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 5. Отправляем токен в cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,                                  // JavaScript не видит cookie
		Secure:   strings.HasPrefix(h.BaseURL, "https"), // на https только по защищённому каналу
		SameSite: http.SameSiteLaxMode,                  // защита от чужих сайтов
	})

	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// CurrentUser смотрит cookie и говорит, кто вошёл.
// Возвращает id, email и true, если пользователь вошёл.
func (h *Handler) CurrentUser(r *http.Request) (int64, string, bool) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return 0, "", false // cookie нет, значит гость
	}

	var userID int64
	var email string
	err = h.Pool.QueryRow(r.Context(),
		`SELECT u.id, u.email
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1 AND s.expires_at > now() AND u.banned_at IS NULL`,
		HashToken(cookie.Value)).Scan(&userID, &email)
	if err != nil {
		return 0, "", false
	}
	return userID, email, true
}

// Account показывает страницу аккаунта (только для вошедших)
func (h *Handler) Account(w http.ResponseWriter, r *http.Request) {
	_, email, ok := h.CurrentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := h.Tmpl.ExecuteTemplate(w, "account.html", AccountData{Email: email})
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}

// Logout удаляет сессию и cookie
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		h.Pool.Exec(r.Context(),
			`DELETE FROM sessions WHERE token_hash = $1`, HashToken(cookie.Value))
	}

	// Просим браузер удалить cookie (MaxAge -1 значит "удалить")
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// showLogin выводит страницу входа
func (h *Handler) showLogin(w http.ResponseWriter, status int, data LoginData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := h.Tmpl.ExecuteTemplate(w, "login.html", data)
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}
