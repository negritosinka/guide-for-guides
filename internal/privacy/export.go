package privacy

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Структуры описывают, как выглядит JSON-файл с данными пользователя
type exportAccount struct {
	ID              int64      `json:"id"`
	Email           string     `json:"email"`
	CreatedAt       time.Time  `json:"created_at"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	Flags           int        `json:"flags_count"`
}

type exportBookmark struct {
	TutorialID int64     `json:"tutorial_id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"created_at"`
}

type exportReview struct {
	TutorialID int64      `json:"tutorial_id"`
	Title      string     `json:"tutorial_title"`
	Rating     int        `json:"rating"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	RemovedAt  *time.Time `json:"removed_at"`
}

type exportReport struct {
	Kind      string    `json:"kind"`
	Reason    string    `json:"reason"`
	Details   string    `json:"details"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type exportAppeal struct {
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type exportStaffAction struct {
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   int64     `json:"target_id"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}

type exportData struct {
	ExportedAt   time.Time           `json:"exported_at"`
	Account      exportAccount       `json:"account"`
	Roles        []string            `json:"roles"`
	Bookmarks    []exportBookmark    `json:"bookmarks"`
	Reviews      []exportReview      `json:"reviews"`
	Reports      []exportReport      `json:"reports_submitted"`
	Appeals      []exportAppeal      `json:"appeals"`
	StaffActions []exportStaffAction `json:"staff_actions"`
}

// fail записывает ошибку в лог и показывает пользователю общую ошибку
func fail(w http.ResponseWriter, err error) {
	log.Println("ошибка экспорта:", err)
	http.Error(w, "Servera kļūda", http.StatusInternalServerError)
}

// Export отдаёт все данные пользователя одним JSON-файлом.
// Хеш пароля в файл не попадает: это секрет, а не данные для пользователя.
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	userID, email, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	ctx := r.Context()

	// Пустые списки, чтобы в JSON было [], а не null
	data := exportData{
		ExportedAt:   time.Now(),
		Roles:        []string{},
		Bookmarks:    []exportBookmark{},
		Reviews:      []exportReview{},
		Reports:      []exportReport{},
		Appeals:      []exportAppeal{},
		StaffActions: []exportStaffAction{},
	}

	// 1. Аккаунт
	data.Account.ID = userID
	data.Account.Email = email
	err := h.Pool.QueryRow(ctx,
		`SELECT created_at, email_verified_at, flags_count FROM users WHERE id = $1`,
		userID).Scan(&data.Account.CreatedAt, &data.Account.EmailVerifiedAt, &data.Account.Flags)
	if err != nil {
		fail(w, err)
		return
	}

	// 2. Роли
	rows, err := h.Pool.Query(ctx, `SELECT role FROM user_roles WHERE user_id = $1`, userID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var role string
		if rows.Scan(&role) == nil {
			data.Roles = append(data.Roles, role)
		}
	}
	rows.Close()

	// 3. Закладки
	rows, err = h.Pool.Query(ctx,
		`SELECT t.id, COALESCE(v.title, ''), t.url, bm.created_at
		 FROM bookmarks bm
		 JOIN tutorials t ON t.id = bm.tutorial_id
		 LEFT JOIN tutorial_versions v ON v.tutorial_id = t.id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 WHERE bm.user_id = $1 ORDER BY bm.created_at`, userID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var b exportBookmark
		if rows.Scan(&b.TutorialID, &b.Title, &b.URL, &b.CreatedAt) == nil {
			data.Bookmarks = append(data.Bookmarks, b)
		}
	}
	rows.Close()

	// 4. Отзывы (в том числе убранные модератором)
	rows, err = h.Pool.Query(ctx,
		`SELECT r.tutorial_id, COALESCE(v.title, ''), r.rating, r.body,
		        r.created_at, r.updated_at, r.removed_at
		 FROM reviews r
		 LEFT JOIN tutorial_versions v ON v.tutorial_id = r.tutorial_id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = r.tutorial_id)
		 WHERE r.user_id = $1 ORDER BY r.id`, userID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var rv exportReview
		if rows.Scan(&rv.TutorialID, &rv.Title, &rv.Rating, &rv.Body,
			&rv.CreatedAt, &rv.UpdatedAt, &rv.RemovedAt) == nil {
			data.Reviews = append(data.Reviews, rv)
		}
	}
	rows.Close()

	// 5. Жалобы, которые подал пользователь
	rows, err = h.Pool.Query(ctx,
		`SELECT kind, reason, details, status, created_at
		 FROM reports WHERE reporter_id = $1 ORDER BY id`, userID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var rp exportReport
		if rows.Scan(&rp.Kind, &rp.Reason, &rp.Details, &rp.Status, &rp.CreatedAt) == nil {
			data.Reports = append(data.Reports, rp)
		}
	}
	rows.Close()

	// 6. Апелляции
	rows, err = h.Pool.Query(ctx,
		`SELECT message, status, created_at FROM appeals WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var a exportAppeal
		if rows.Scan(&a.Message, &a.Status, &a.CreatedAt) == nil {
			data.Appeals = append(data.Appeals, a)
		}
	}
	rows.Close()

	// 7. Действия пользователя как сотрудника (у обычных пользователей пусто)
	rows, err = h.Pool.Query(ctx,
		`SELECT action, target_type, COALESCE(target_id, 0), details, created_at
		 FROM staff_actions WHERE actor_id = $1 ORDER BY id`, userID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var s exportStaffAction
		if rows.Scan(&s.Action, &s.TargetType, &s.TargetID, &s.Details, &s.CreatedAt) == nil {
			data.StaffActions = append(data.StaffActions, s)
		}
	}
	rows.Close()

	// 8. Отдаём как файл для скачивания
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="mani-dati.json"`)
	w.Header().Set("Cache-Control", "no-store")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	err = enc.Encode(data)
	if err != nil {
		log.Println("ошибка записи JSON:", err)
	}
}
