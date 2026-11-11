package auth

import (
	"net/http"
	"strings"
)

// CheckLogin проверяет email и пароль для форм вне страницы входа
// (например, апелляция заблокированного пользователя).
// Ограничение попыток работает так же, как на странице входа.
// Возвращает id пользователя и true, если всё верно.
func (h *Handler) CheckLogin(r *http.Request, email string, password string) (int64, bool) {
	if h.tooManyAttempts(r, email) {
		return 0, false
	}

	var userID int64
	var hash string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, password_hash FROM users WHERE lower(email) = lower($1)`,
		email).Scan(&userID, &hash)

	ok := false
	if err == nil {
		ok = CheckPassword(password, hash)
	} else {
		// Пользователя нет: всё равно считаем хеш, чтобы ответ шёл столько же времени
		HashPassword("neeksistējošs-lietotājs")
	}

	if !ok {
		// Записываем неудачную попытку
		h.Pool.Exec(r.Context(),
			`INSERT INTO login_attempts (email_hash, ip) VALUES ($1, $2)`,
			HashToken(strings.ToLower(email)), clientIP(r))
		return 0, false
	}
	return userID, true
}
