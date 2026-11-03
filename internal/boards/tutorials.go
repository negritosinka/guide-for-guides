package boards

import (
	"log"
	"net/http"
)

// TutorialRow это один туториал в списке
type TutorialRow struct {
	Title      string
	URL        string
	Source     string
	Categories []string
}

// BoardPage это данные для страницы доски
type BoardPage struct {
	Board     Board
	Tutorials []TutorialRow
}

// tutorialsOf достаёт туториалы доски (последняя версия каждого)
func (h *Handler) tutorialsOf(r *http.Request, slug string) []TutorialRow {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT v.title, t.url, s.name, v.categories
		 FROM tutorials t
		 JOIN boards b ON b.id = t.board_id
		 JOIN sources s ON s.id = t.source_id
		 JOIN tutorial_versions v ON v.tutorial_id = t.id
		 WHERE b.slug = $1
		   AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 ORDER BY t.id DESC LIMIT 50`, slug)
	if err != nil {
		log.Println("ошибка базы:", err)
		return nil
	}
	defer rows.Close()

	var list []TutorialRow
	for rows.Next() {
		var t TutorialRow
		err := rows.Scan(&t.Title, &t.URL, &t.Source, &t.Categories)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		list = append(list, t)
	}
	return list
}
