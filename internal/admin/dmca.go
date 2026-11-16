package admin

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// hideInTx скрывает туториал и закрывает все открытые жалобы на него.
// Работает внутри уже начатой транзакции.
func (h *Handler) hideInTx(ctx context.Context, tx pgx.Tx, staffID int64, tutorialID int64) error {
	_, err := tx.Exec(ctx,
		`UPDATE tutorials SET hidden_at = now() WHERE id = $1 AND hidden_at IS NULL`, tutorialID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE reports SET status = 'done', handled_by = $1, handled_at = now()
		 WHERE tutorial_id = $2 AND status = 'open'`, staffID, tutorialID)
	return err
}

// hideTutorial скрывает туториал по жалобе (транзакция + запись в журнал)
func (h *Handler) hideTutorial(ctx context.Context, staffID int64, tutorialID int64, note string) error {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = h.hideInTx(ctx, tx, staffID, tutorialID)
	if err != nil {
		return err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	h.logAction(ctx, staffID, "hide_tutorial", "tutorial", tutorialID, note)
	return nil
}

// ---------- DMCA-заявки ----------

type DMCARow struct {
	ID          int64
	Name        string
	Email       string
	TutorialID  int64
	Title       string
	URL         string
	Hidden      bool
	Description string
	OriginalURL string
	Signature   string
	Date        string
}

type DMCAListData struct {
	Rows []DMCARow
}

// DMCAList показывает открытые заявки (модераторам и админам)
func (h *Handler) DMCAList(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkStaff(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT d.id, d.name, d.email, d.tutorial_id, COALESCE(v.title, ''), t.url,
		        t.hidden_at IS NOT NULL, d.description, d.original_url, d.signature, d.created_at
		 FROM dmca_requests d
		 JOIN tutorials t ON t.id = d.tutorial_id
		 LEFT JOIN tutorial_versions v ON v.tutorial_id = t.id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 WHERE d.status = 'open'
		 ORDER BY d.id LIMIT 100`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data DMCAListData
	for rows.Next() {
		var row DMCARow
		var created time.Time
		err := rows.Scan(&row.ID, &row.Name, &row.Email, &row.TutorialID, &row.Title, &row.URL,
			&row.Hidden, &row.Description, &row.OriginalURL, &row.Signature, &created)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		row.Date = created.Format("2006-01-02 15:04")
		data.Rows = append(data.Rows, row)
	}

	h.page(w, "admin_dmca.html", data)
}

// DMCAResolve принимает заявку (скрывает туториал) или отклоняет её
func (h *Handler) DMCAResolve(w http.ResponseWriter, r *http.Request) {
	staffID, ok := h.checkStaff(w, r)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	if action != "accept" && action != "reject" {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	var tutorialID int64
	err = h.Pool.QueryRow(r.Context(),
		`SELECT tutorial_id FROM dmca_requests WHERE id = $1 AND status = 'open'`,
		id).Scan(&tutorialID)
	if err == pgx.ErrNoRows {
		http.Redirect(w, r, "/admin/dmca", http.StatusSeeOther) // уже обработана
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	status := "rejected"
	if action == "accept" {
		status = "accepted"
	}

	// Скрытие туториала и закрытие заявки делаем в одной транзакции
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	if action == "accept" {
		err = h.hideInTx(r.Context(), tx, staffID, tutorialID)
		if err != nil {
			log.Println("ошибка:", err)
			http.Error(w, "Servera kļūda", http.StatusInternalServerError)
			return
		}
	}

	_, err = tx.Exec(r.Context(),
		`UPDATE dmca_requests SET status = $1, handled_by = $2, handled_at = now() WHERE id = $3`,
		status, staffID, id)
	if err != nil {
		log.Println("ошибка:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	err = tx.Commit(r.Context())
	if err != nil {
		log.Println("ошибка:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.logAction(r.Context(), staffID, action+"_dmca", "dmca", id,
		"tutorial="+strconv.FormatInt(tutorialID, 10))

	http.Redirect(w, r, "/admin/dmca", http.StatusSeeOther)
}

// ---------- Скрытые туториалы (только админам) ----------

type HiddenRow struct {
	ID    int64
	Title string
	URL   string
}

type HiddenData struct {
	Rows []HiddenRow
}

// HiddenList показывает скрытые туториалы
func (h *Handler) HiddenList(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT t.id, COALESCE(v.title, ''), t.url
		 FROM tutorials t
		 LEFT JOIN tutorial_versions v ON v.tutorial_id = t.id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 WHERE t.hidden_at IS NOT NULL
		 ORDER BY t.hidden_at DESC LIMIT 200`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data HiddenData
	for rows.Next() {
		var row HiddenRow
		err := rows.Scan(&row.ID, &row.Title, &row.URL)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		data.Rows = append(data.Rows, row)
	}

	h.page(w, "admin_hidden.html", data)
}

// Restore возвращает скрытый туториал (только админам)
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`UPDATE tutorials SET hidden_at = NULL WHERE id = $1 AND hidden_at IS NOT NULL`, id)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.logAction(r.Context(), adminID, "restore_tutorial", "tutorial", id, "")

	http.Redirect(w, r, "/admin/hidden", http.StatusSeeOther)
}
