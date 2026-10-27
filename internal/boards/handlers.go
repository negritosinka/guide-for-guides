package boards

import (
	"html/template"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/auth"
)

type Handler struct {
	Pool *pgxpool.Pool
	Tmpl *template.Template
	Auth *auth.Handler // нужен, чтобы узнать, кто вошёл
}

// Board это одна доска из базы
type Board struct {
	Slug         string
	Name         string
	Description  string
	Status       string
	RequiredRole string
}

type ListData struct {
	Boards []Board
}

type MessageData struct {
	Title string
	Text  string
}

// rolesOf возвращает роли текущего пользователя (у гостя их нет)
func (h *Handler) rolesOf(r *http.Request) []string {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		return nil
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT role FROM user_roles WHERE user_id = $1`, userID)
	if err != nil {
		log.Println("ошибка базы:", err)
		return nil
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if rows.Scan(&role) == nil {
			roles = append(roles, role)
		}
	}
	return roles
}

// hasRole проверяет, есть ли роль в списке
func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// access решает, пускать ли пользователя на доску.
// Возвращает: "ok", "hidden", "maintenance" или "noaccess".
func access(b Board, roles []string) string {
	isAdmin := hasRole(roles, "admin")
	isStaff := isAdmin || hasRole(roles, "moderator")

	if b.Status == "disabled" {
		return "hidden" // выключена для всех
	}
	if b.Status == "maintenance" && !isStaff {
		return "maintenance"
	}
	if b.RequiredRole != "" && !hasRole(roles, b.RequiredRole) && !isAdmin {
		return "noaccess"
	}
	return "ok"
}

// List показывает доски, доступные этому пользователю
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT slug, name, description, status, COALESCE(required_role, '')
		 FROM boards ORDER BY id`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	roles := h.rolesOf(r)

	var data ListData
	for rows.Next() {
		var b Board
		err := rows.Scan(&b.Slug, &b.Name, &b.Description, &b.Status, &b.RequiredRole)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		// В список попадают только доски, на которые можно зайти
		if access(b, roles) == "ok" {
			data.Boards = append(data.Boards, b)
		}
	}

	h.page(w, http.StatusOK, "boards.html", data)
}

// Show показывает одну доску
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var b Board
	err := h.Pool.QueryRow(r.Context(),
		`SELECT slug, name, description, status, COALESCE(required_role, '')
		 FROM boards WHERE slug = $1`, slug).
		Scan(&b.Slug, &b.Name, &b.Description, &b.Status, &b.RequiredRole)
	if err == pgx.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	switch access(b, h.rolesOf(r)) {
	case "hidden":
		http.NotFound(w, r)
	case "maintenance":
		h.page(w, http.StatusServiceUnavailable, "message.html", MessageData{
			Title: "Apkope",
			Text:  "Sadaļa pašlaik tiek apkalpota. Lūdzu, atgriezieties vēlāk.",
		})
	case "noaccess":
		h.page(w, http.StatusForbidden, "message.html", MessageData{
			Title: "Nav piekļuves",
			Text:  "Jums nav piekļuves šai sadaļai.",
		})
	default:
		h.page(w, http.StatusOK, "board.html", b)
	}
}

// page показывает страницу по имени шаблона
func (h *Handler) page(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := h.Tmpl.ExecuteTemplate(w, name, data)
	if err != nil {
		log.Println("ошибка шаблона:", err)
	}
}
