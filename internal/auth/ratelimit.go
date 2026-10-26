package auth

import (
	"log"
	"net"
	"net/http"
	"strings"
)

// Сколько неудачных попыток разрешено за 15 минут
const maxFailures = 5

// clientIP достаёт IP-адрес пользователя (без порта)
func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// tooManyAttempts возвращает true, если попыток уже слишком много
func (h *Handler) tooManyAttempts(r *http.Request, email string) bool {
	// Сначала удаляем старые записи (старше суток), чтобы не хранить лишнего
	h.Pool.Exec(r.Context(),
		`DELETE FROM login_attempts WHERE created_at < now() - interval '1 day'`)

	// Считаем неудачи за 15 минут: по этому email или с этого IP
	var count int
	err := h.Pool.QueryRow(r.Context(),
		`SELECT count(*) FROM login_attempts
		 WHERE created_at > now() - interval '15 minutes'
		   AND (email_hash = $1 OR ip = $2)`,
		HashToken(strings.ToLower(email)), clientIP(r)).Scan(&count)
	if err != nil {
		log.Println("ошибка базы:", err)
		return false
	}

	return count >= maxFailures
}

// loginFailed записывает неудачную попытку и показывает общую ошибку
func (h *Handler) loginFailed(w http.ResponseWriter, r *http.Request, email string) {
	h.Pool.Exec(r.Context(),
		`INSERT INTO login_attempts (email_hash, ip) VALUES ($1, $2)`,
		HashToken(strings.ToLower(email)), clientIP(r))

	h.showLogin(w, http.StatusUnauthorized, LoginData{Error: loginError, Email: email})
}
