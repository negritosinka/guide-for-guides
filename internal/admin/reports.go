package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- Очередь жалоб ----------

type ReportRow struct {
	ID         int64
	Kind       string // "content" или "bug"
	Reason     string
	Details    string
	Date       string
	ReporterID int64
	ReviewID   int64
	Rating     int
	ReviewBody string
	AuthorID   int64
}

type ReportsData struct {
	Rows []ReportRow
}

// Reports показывает открытые жалобы (модераторам и админам)
func (h *Handler) Reports(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkStaff(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT rp.id, rp.kind, rp.reason, rp.details, rp.created_at,
		        COALESCE(rp.reporter_id, 0),
		        COALESCE(rv.id, 0), COALESCE(rv.rating, 0),
		        COALESCE(rv.body, ''), COALESCE(rv.user_id, 0)
		 FROM reports rp
		 LEFT JOIN reviews rv ON rv.id = rp.review_id
		 WHERE rp.status = 'open'
		 ORDER BY rp.id LIMIT 100`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data ReportsData
	for rows.Next() {
		var row ReportRow
		var created time.Time
		err := rows.Scan(&row.ID, &row.Kind, &row.Reason, &row.Details, &created,
			&row.ReporterID, &row.ReviewID, &row.Rating, &row.ReviewBody, &row.AuthorID)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		row.Date = created.Format("2006-01-02 15:04")
		data.Rows = append(data.Rows, row)
	}

	h.page(w, "admin_reports.html", data)
}

// ResolveReport обрабатывает решение модератора по жалобе
func (h *Handler) ResolveReport(w http.ResponseWriter, r *http.Request) {
	staffID, ok := h.checkStaff(w, r)
	if !ok {
		return
	}

	reportID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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

	// Достаём открытую жалобу
	var kind string
	var reviewID *int64 // указатель, потому что у баг-репорта отзыва нет
	err = h.Pool.QueryRow(r.Context(),
		`SELECT kind, review_id FROM reports WHERE id = $1 AND status = 'open'`,
		reportID).Scan(&kind, &reviewID)
	if err == pgx.ErrNoRows {
		http.Redirect(w, r, "/admin/reports", http.StatusSeeOther) // уже обработана
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	if action == "remove" && kind == "content" && reviewID != nil {
		err = h.removeReview(r.Context(), staffID, *reviewID)
	} else if action == "dismiss" || (action == "done" && kind == "bug") {
		status := "dismissed"
		if action == "done" {
			status = "done"
		}
		_, err = h.Pool.Exec(r.Context(),
			`UPDATE reports SET status = $1, handled_by = $2, handled_at = now() WHERE id = $3`,
			status, staffID, reportID)
		h.logAction(r.Context(), staffID, action+"_report", "report", reportID, "")
	} else {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Println("ошибка:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/reports", http.StatusSeeOther)
}

// removeReview убирает отзыв, даёт автору флаг и блокирует его,
// если флагов стало слишком много. Всё в одной транзакции.
func (h *Handler) removeReview(ctx context.Context, staffID int64, reviewID int64) error {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // после успешного Commit ничего не делает

	// 1. Помечаем отзыв убранным (только если он ещё не убран)
	var authorID int64
	err = tx.QueryRow(ctx,
		`UPDATE reviews SET removed_at = now()
		 WHERE id = $1 AND removed_at IS NULL RETURNING user_id`,
		reviewID).Scan(&authorID)

	if err == pgx.ErrNoRows {
		// Отзыв уже убран раньше. Флаг второй раз не даём, просто закрываем жалобы.
		_, err = tx.Exec(ctx,
			`UPDATE reports SET status = 'done', handled_by = $1, handled_at = now()
			 WHERE review_id = $2 AND status = 'open'`, staffID, reviewID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}

	// 2. Флаг автору
	_, err = tx.Exec(ctx,
		`INSERT INTO user_flags (user_id, review_id, created_by) VALUES ($1, $2, $3)`,
		authorID, reviewID, staffID)
	if err != nil {
		return err
	}

	var flags int
	err = tx.QueryRow(ctx,
		`UPDATE users SET flags_count = flags_count + 1 WHERE id = $1 RETURNING flags_count`,
		authorID).Scan(&flags)
	if err != nil {
		return err
	}

	// 3. Блокировка при достижении лимита (админов по флагам не блокируем)
	banned := false
	if flags >= h.MaxFlags && !h.hasRole(ctx, authorID, "admin") {
		_, err = tx.Exec(ctx,
			`UPDATE users SET banned_at = now() WHERE id = $1 AND banned_at IS NULL`, authorID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, authorID)
		if err != nil {
			return err
		}
		banned = true
	}

	// 4. Все открытые жалобы на этот отзыв считаем обработанными
	_, err = tx.Exec(ctx,
		`UPDATE reports SET status = 'done', handled_by = $1, handled_at = now()
		 WHERE review_id = $2 AND status = 'open'`, staffID, reviewID)
	if err != nil {
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	// 5. Журнал персонала
	h.logAction(ctx, staffID, "remove_review", "review", reviewID,
		fmt.Sprintf("author=%d, flags=%d", authorID, flags))
	if banned {
		h.logAction(ctx, staffID, "auto_ban", "user", authorID, "flags limit reached")
	}
	return nil
}

// ---------- Апелляции ----------

type AppealRow struct {
	ID           int64
	UserID       int64
	Banned       bool
	Flags        int
	Message      string
	Date         string
	ReviewID     int64 // 0 значит апелляция на блокировку аккаунта
	ReviewBody   string
	ReviewRating int
}

type AppealsData struct {
	Rows []AppealRow
}

// Appeals показывает открытые апелляции
func (h *Handler) Appeals(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkStaff(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT a.id, a.user_id, u.banned_at IS NOT NULL, u.flags_count,
		        a.message, a.created_at,
		        COALESCE(rv.id, 0), COALESCE(rv.body, ''), COALESCE(rv.rating, 0)
		 FROM appeals a
		 JOIN users u ON u.id = a.user_id
		 LEFT JOIN reviews rv ON rv.id = a.review_id
		 WHERE a.status = 'open'
		 ORDER BY a.id LIMIT 100`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data AppealsData
	for rows.Next() {
		var row AppealRow
		var created time.Time
		err := rows.Scan(&row.ID, &row.UserID, &row.Banned, &row.Flags,
			&row.Message, &created, &row.ReviewID, &row.ReviewBody, &row.ReviewRating)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		row.Date = created.Format("2006-01-02 15:04")
		data.Rows = append(data.Rows, row)
	}

	h.page(w, "admin_appeals.html", data)
}

// ResolveAppeal принимает или отклоняет апелляцию
func (h *Handler) ResolveAppeal(w http.ResponseWriter, r *http.Request) {
	staffID, ok := h.checkStaff(w, r)
	if !ok {
		return
	}

	appealID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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

	// Достаём открытую апелляцию
	var userID int64
	var reviewID *int64 // пусто для апелляции на блокировку
	err = h.Pool.QueryRow(r.Context(),
		`SELECT user_id, review_id FROM appeals WHERE id = $1 AND status = 'open'`,
		appealID).Scan(&userID, &reviewID)
	if err == pgx.ErrNoRows {
		http.Redirect(w, r, "/admin/appeals", http.StatusSeeOther)
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	err = h.finishAppeal(r.Context(), staffID, appealID, userID, reviewID, action)
	if err != nil {
		log.Println("ошибка:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.logAction(r.Context(), staffID, action+"_appeal", "appeal", appealID,
		fmt.Sprintf("user=%d", userID))

	http.Redirect(w, r, "/admin/appeals", http.StatusSeeOther)
}

// finishAppeal выполняет решение по апелляции в одной транзакции
func (h *Handler) finishAppeal(ctx context.Context, staffID int64, appealID int64,
	userID int64, reviewID *int64, action string) error {

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	status := "rejected"

	if action == "accept" {
		status = "accepted"

		if reviewID != nil {
			// Возвращаем отзыв
			_, err = tx.Exec(ctx,
				`UPDATE reviews SET removed_at = NULL WHERE id = $1`, *reviewID)
			if err != nil {
				return err
			}
			// Убираем флаг, который был выдан за этот отзыв
			tag, err := tx.Exec(ctx, `DELETE FROM user_flags WHERE review_id = $1`, *reviewID)
			if err != nil {
				return err
			}
			removed := int(tag.RowsAffected())
			if removed > 0 {
				_, err = tx.Exec(ctx,
					`UPDATE users SET flags_count = GREATEST(flags_count - $1, 0) WHERE id = $2`,
					removed, userID)
				if err != nil {
					return err
				}
			}
		} else {
			// Апелляция на блокировку: снимаем бан и обнуляем флаги
			_, err = tx.Exec(ctx,
				`UPDATE users SET banned_at = NULL, flags_count = 0 WHERE id = $1`, userID)
			if err != nil {
				return err
			}
		}
	}

	_, err = tx.Exec(ctx,
		`UPDATE appeals SET status = $1, handled_by = $2, handled_at = now() WHERE id = $3`,
		status, staffID, appealID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
