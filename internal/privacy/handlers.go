package privacy

import (
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/auth"
)

type Handler struct {
	Pool *pgxpool.Pool
	Tmpl *template.Template
	Auth *auth.Handler // нужен, чтобы узнать, кто вошёл
}

type MessageData struct {
	Title string
	Text  string
}

type DeleteData struct {
	Error string
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

// message показывает страницу с заголовком и текстом
func (h *Handler) message(w http.ResponseWriter, status int, title string, text string) {
	h.page(w, status, "message.html", MessageData{Title: title, Text: text})
}

// Policy показывает страницу политики конфиденциальности (доступна всем)
func (h *Handler) Policy(w http.ResponseWriter, r *http.Request) {
	h.page(w, http.StatusOK, "privacy.html", nil)
}

// DeleteForm показывает форму удаления аккаунта
func (h *Handler) DeleteForm(w http.ResponseWriter, r *http.Request) {
	_, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.page(w, http.StatusOK, "delete_account.html", DeleteData{})
}

// DeleteSubmit удаляет аккаунт вместе с данными пользователя
func (h *Handler) DeleteSubmit(w http.ResponseWriter, r *http.Request) {
	userID, email, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	// 1. Пользователь должен подтвердить, что понимает последствия
	if r.FormValue("confirm") != "on" {
		h.page(w, http.StatusBadRequest, "delete_account.html",
			DeleteData{Error: "Atzīmējiet apstiprinājuma rūtiņu."})
		return
	}

	// 2. Администратора удалять нельзя (иначе можно остаться без админов)
	var isAdmin bool
	err = h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = $1 AND role = 'admin')`,
		userID).Scan(&isAdmin)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	if isAdmin {
		h.message(w, http.StatusForbidden, "Nav iespējams",
			"Administratora kontu šeit dzēst nevar. Sazinieties ar citu administratoru.")
		return
	}

	// 3. Проверяем пароль (с тем же ограничением попыток, что и при входе)
	_, ok := h.Auth.CheckLogin(r, email, r.FormValue("password"))
	if !ok {
		h.page(w, http.StatusUnauthorized, "delete_account.html",
			DeleteData{Error: "Nepareiza parole (vai pārāk daudz mēģinājumu)."})
		return
	}

	// 4. Удаляем пользователя. Остальное удалится само (ON DELETE CASCADE).
	_, err = h.Pool.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		log.Println("ошибка удаления:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 5. Стираем попытки входа с этим email (в базе они хранятся как хеш)
	h.Pool.Exec(r.Context(),
		`DELETE FROM login_attempts WHERE email_hash = $1`,
		auth.HashToken(strings.ToLower(email)))

	// 6. Просим браузер удалить cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	h.message(w, http.StatusOK, "Konts dzēsts",
		"Jūsu konts un ar to saistītie dati ir dzēsti.")
}
