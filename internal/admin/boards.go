package admin

import (
	"log"
	"net/http"
	"strconv"
)

// BoardRow это одна строка таблицы досок в админке
type BoardRow struct {
	ID           int64
	Name         string
	Status       string
	RequiredRole string
	Uses         string // какие доски использует эта
	UsedBy       string // какие доски используют эту
}

type BoardsData struct {
	Boards []BoardRow
}

// Boards показывает все доски (только админам)
func (h *Handler) Boards(w http.ResponseWriter, r *http.Request) {
	_, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT b.id, b.name, b.status, COALESCE(b.required_role, ''),
		   COALESCE((SELECT string_agg(d.name, ', ')
		             FROM board_dependencies bd JOIN boards d ON d.id = bd.depends_on_id
		             WHERE bd.board_id = b.id), ''),
		   COALESCE((SELECT string_agg(u.name, ', ')
		             FROM board_dependencies bd JOIN boards u ON u.id = bd.board_id
		             WHERE bd.depends_on_id = b.id), '')
		 FROM boards b ORDER BY b.id`)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data BoardsData
	for rows.Next() {
		var b BoardRow
		err := rows.Scan(&b.ID, &b.Name, &b.Status, &b.RequiredRole, &b.Uses, &b.UsedBy)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		data.Boards = append(data.Boards, b)
	}

	h.page(w, "admin_boards.html", data)
}

// UpdateBoard меняет статус и роль доски
func (h *Handler) UpdateBoard(w http.ResponseWriter, r *http.Request) {
	adminID, ok := h.checkAdmin(w, r)
	if !ok {
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	boardID, err := strconv.ParseInt(r.FormValue("board_id"), 10, 64)
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")
	role := r.FormValue("required_role")

	// Принимаем только разрешённые значения
	if status != "active" && status != "maintenance" && status != "disabled" {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	if role != "" && role != "moderator" && role != "admin" {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	// NULLIF превращает пустую строку в NULL (то есть "без ограничения")
	_, err = h.Pool.Exec(r.Context(),
		`UPDATE boards SET status = $1, required_role = NULLIF($2::text, '') WHERE id = $3`,
		status, role, boardID)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.logAction(r.Context(), adminID, "update_board", "board", boardID,
		"status="+status+", role="+role)

	http.Redirect(w, r, "/admin/boards", http.StatusSeeOther)
}
