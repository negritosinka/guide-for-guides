package admin

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/auth"
)

// Handler хранит всё, что нужно страницам админки
type Handler struct {
	Pool     *pgxpool.Pool
	Tmpl     *template.Template
	Auth     *auth.Handler // нужен, чтобы узнать, кто вошёл
	MaxFlags int           // после стольких флагов аккаунт блокируется
}

// hasRole проверяет, есть ли у пользователя роль
func (h *Handler) hasRole(ctx context.Context, userID int64, role string) bool {
	var found bool
	err := h.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = $1 AND role = $2)`,
		userID, role).Scan(&found)
	if err != nil {
		return false
	}
	return found
}

// checkStaff пускает только модераторов и админов.
// Возвращает id пользователя и true, если можно продолжать.
func (h *Handler) checkStaff(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return 0, false
	}

	isStaff := h.hasRole(r.Context(), userID, "admin") ||
		h.hasRole(r.Context(), userID, "moderator")
	if !isStaff {
		http.NotFound(w, r) // обычному пользователю не показываем, что админка есть
		return 0, false
	}
	return userID, true
}

// checkAdmin пускает только админов
func (h *Handler) checkAdmin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return 0, false
	}

	if !h.hasRole(r.Context(), userID, "admin") {
		http.NotFound(w, r)
		return 0, false
	}
	return userID, true
}

// logAction записывает действие персонала в журнал
func (h *Handler) logAction(ctx context.Context, actorID int64, action string, targetType string, targetID int64, details string) {
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO staff_actions (actor_id, action, target_type, target_id, details)
		 VALUES ($1, $2, $3, $4, $5)`,
		actorID, action, targetType, targetID, details)
	if err != nil {
		log.Println("ошибка журнала:", err)
	}
}

// page показывает страницу по имени шаблона
func (h *Handler) page(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := h.Tmpl.ExecuteTemplate(w, name, data)
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}

// DashboardData для главной страницы админки
type DashboardData struct {
	IsAdmin bool
}

// Dashboard это главная страница админки
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.checkStaff(w, r)
	if !ok {
		return
	}
	isAdmin := h.hasRole(r.Context(), userID, "admin")
	h.page(w, "admin.html", DashboardData{IsAdmin: isAdmin})
}

// LogRow это одна строка журнала
type LogRow struct {
	Time    string
	Who     string
	Action  string
	Target  string
	Details string
}

// LogData для страницы журнала
type LogData struct {
	Rows []LogRow
}

// Log показывает последние 100 действий персонала (только админам)
func (h *Handler) Log(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT a.created_at, COALESCE(u.email, '(dzēsts konts)'), a.action,
		        a.target_type, COALESCE(a.target_id, 0), a.details
		 FROM staff_actions a
		 LEFT JOIN users u ON u.id = a.actor_id
		 ORDER BY a.id DESC LIMIT 100`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data LogData
	for rows.Next() {
		var t time.Time
		var row LogRow
		var targetType string
		var targetID int64

		err := rows.Scan(&t, &row.Who, &row.Action, &targetType, &targetID, &row.Details)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		row.Time = t.Format("2006-01-02 15:04:05")
		row.Target = targetType + " #" + strconv.FormatInt(targetID, 10)
		data.Rows = append(data.Rows, row)
	}

	h.page(w, "admin_log.html", data)
}
