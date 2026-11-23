package auth

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

// Сколько неудачных попыток разрешено за 15 минут
const maxFailures = 5

// clientIP достаёт IP-адрес пользователя (без порта)
// clientIP достаёт IP-адрес пользователя (без порта).
// За прокси (на Render) настоящий адрес лежит в заголовке X-Forwarded-For.
func clientIP(r *http.Request) string {
	if os.Getenv("TRUST_PROXY") == "true" {
		forwarded := r.Header.Get("X-Forwarded-For")
		if forwarded != "" {
			// Заголовок выглядит как "клиент, прокси1, прокси2". Берём первый.
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if net.ParseIP(first) != nil {
				return first
			}
		}
	}

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
