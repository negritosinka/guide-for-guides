package admin

import (
	"log"
	"net/http"
	"strconv"
)

// UserRow это одна строка таблицы пользователей
type UserRow struct {
	ID     int64
	Email  string
	Roles  string
	Banned bool
}

// UsersData для страницы пользователей
type UsersData struct {
	Users []UserRow
	Error string
}

// Users показывает список пользователей (только админам)
func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}
	h.showUsers(w, r, "")
}

// showUsers достаёт пользователей из базы и показывает страницу
func (h *Handler) showUsers(w http.ResponseWriter, r *http.Request, errText string) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT u.id, u.email, u.banned_at IS NOT NULL,
		        COALESCE(string_agg(ur.role, ', '), '')
		 FROM users u
		 LEFT JOIN user_roles ur ON ur.user_id = u.id
		 GROUP BY u.id
		 ORDER BY u.id LIMIT 200`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	data := UsersData{Error: errText}
	for rows.Next() {
		var u UserRow
		err := rows.Scan(&u.ID, &u.Email, &u.Banned, &u.Roles)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		data.Users = append(data.Users, u)
	}

	h.page(w, "admin_users.html", data)
}

// ChangeRole выдаёт или забирает роль moderator.
// Роль admin через сайт выдать нельзя, это защита от ошибок.
func (h *Handler) ChangeRole(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")

	if action == "grant" {
		_, err = h.Pool.Exec(r.Context(),
			`INSERT INTO user_roles (user_id, role) VALUES ($1, 'moderator')
			 ON CONFLICT DO NOTHING`, targetID)
	} else if action == "revoke" {
		_, err = h.Pool.Exec(r.Context(),
			`DELETE FROM user_roles WHERE user_id = $1 AND role = 'moderator'`, targetID)
	} else {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// Записываем действие в журнал
	h.logAction(r.Context(), adminID, action+"_moderator", "user", targetID, "")

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// ChangeBan блокирует или разблокирует пользователя
func (h *Handler) ChangeBan(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")

	// Защита от ошибок: себя и других админов блокировать нельзя
	if targetID == adminID {
		h.showUsers(w, r, "Jūs nevarat bloķēt pats sevi.")
		return
	}
	if h.hasRole(r.Context(), targetID, "admin") {
		h.showUsers(w, r, "Administratorus bloķēt nevar.")
		return
	}

	if action == "ban" {
		_, err = h.Pool.Exec(r.Context(),
			`UPDATE users SET banned_at = now() WHERE id = $1`, targetID)
		// Сразу выкидываем заблокированного: удаляем его сессии
		h.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE user_id = $1`, targetID)
	} else if action == "unban" {
		_, err = h.Pool.Exec(r.Context(),
			`UPDATE users SET banned_at = NULL WHERE id = $1`, targetID)
	} else {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.logAction(r.Context(), adminID, action, "user", targetID, "")

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
