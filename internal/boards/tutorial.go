package boards

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// tutorialInfo это туториал вместе с его доской
type tutorialInfo struct {
	ID         int64
	Title      string
	URL        string
	Source     string
	Categories []string
	Board      Board
}

// ReviewRow это один отзыв на странице
type ReviewRow struct {
	ID     int64
	Author string
	Rating int
	Body   string
	Date   string
	Mine   bool // мой ли это отзыв
}

// TutorialPage это все данные для страницы туториала
type TutorialPage struct {
	ID          int64
	Title       string
	URL         string
	Source      string
	Categories  []string
	BoardName   string
	BoardSlug   string
	LoggedIn    bool
	Bookmarked  bool
	Reviews     []ReviewRow
	ReviewCount int
	Average     string
	MyRating    int
	MyBody      string
	Error       string
}

// loadTutorial достаёт туториал по номеру из адреса и проверяет доступ к его доске.
// Если показывать нельзя, сама отвечает пользователю и возвращает false.
func (h *Handler) loadTutorial(w http.ResponseWriter, r *http.Request) (tutorialInfo, bool) {
	var t tutorialInfo

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return t, false
	}
	t.ID = id

	err = h.Pool.QueryRow(r.Context(),
		`SELECT v.title, t.url, s.name, v.categories,
		        b.slug, b.name, b.description, b.status, COALESCE(b.required_role, '')
		 FROM tutorials t
		 JOIN boards b ON b.id = t.board_id
		 JOIN sources s ON s.id = t.source_id
		 JOIN tutorial_versions v ON v.tutorial_id = t.id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 WHERE t.id = $1`, id).
		Scan(&t.Title, &t.URL, &t.Source, &t.Categories,
			&t.Board.Slug, &t.Board.Name, &t.Board.Description, &t.Board.Status, &t.Board.RequiredRole)
	if err == pgx.ErrNoRows {
		http.NotFound(w, r)
		return t, false
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return t, false
	}

	// Та же проверка доступа, что и на странице доски
	switch access(t.Board, h.rolesOf(r)) {
	case "ok":
		return t, true
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
		http.NotFound(w, r)
	}
	return t, false
}

// showTutorial собирает данные и показывает страницу туториала
func (h *Handler) showTutorial(w http.ResponseWriter, r *http.Request, t tutorialInfo, status int, errText string) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)

	data := TutorialPage{
		ID:         t.ID,
		Title:      t.Title,
		URL:        t.URL,
		Source:     t.Source,
		Categories: t.Categories,
		BoardName:  t.Board.Name,
		BoardSlug:  t.Board.Slug,
		LoggedIn:   loggedIn,
		Error:      errText,
	}

	if loggedIn {
		// Есть ли туториал в закладках
		h.Pool.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM bookmarks WHERE user_id = $1 AND tutorial_id = $2)`,
			userID, t.ID).Scan(&data.Bookmarked)

		// Мой отзыв (если его нет, останутся нули и пустая строка)
		h.Pool.QueryRow(r.Context(),
			`SELECT rating, body FROM reviews
			 WHERE user_id = $1 AND tutorial_id = $2 AND removed_at IS NULL`,
			userID, t.ID).Scan(&data.MyRating, &data.MyBody)
	}

	// Количество отзывов и средняя оценка
	var avg float64
	err := h.Pool.QueryRow(r.Context(),
		`SELECT count(*), COALESCE(avg(rating), 0)::float8
		 FROM reviews WHERE tutorial_id = $1 AND removed_at IS NULL`,
		t.ID).Scan(&data.ReviewCount, &avg)
	if err != nil {
		log.Println("ошибка базы:", err)
	}
	data.Average = fmt.Sprintf("%.1f", avg)

	// Сами отзывы (последние 50)
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, user_id, rating, body, created_at FROM reviews
		 WHERE tutorial_id = $1 AND removed_at IS NULL
		 ORDER BY id DESC LIMIT 50`, t.ID)
	if err != nil {
		log.Println("ошибка базы:", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var authorID int64
			var created time.Time
			var rv ReviewRow

			err := rows.Scan(&rv.ID, &authorID, &rv.Rating, &rv.Body, &created)
			if err != nil {
				log.Println("ошибка чтения строки:", err)
				continue
			}
			rv.Author = fmt.Sprintf("Lietotājs #%d", authorID) // email не показываем
			rv.Date = created.Format("2006-01-02")
			rv.Mine = loggedIn && authorID == userID
			data.Reviews = append(data.Reviews, rv)
		}
	}

	h.page(w, status, "tutorial.html", data)
}

// Tutorial показывает страницу туториала
func (h *Handler) Tutorial(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTutorial(w, r)
	if !ok {
		return
	}
	h.showTutorial(w, r, t, http.StatusOK, "")
}

// Bookmark добавляет туториал в закладки или убирает его
func (h *Handler) Bookmark(w http.ResponseWriter, r *http.Request) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	t, ok := h.loadTutorial(w, r)
	if !ok {
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	action := r.FormValue("action")
	if action == "add" {
		_, err = h.Pool.Exec(r.Context(),
			`INSERT INTO bookmarks (user_id, tutorial_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`, userID, t.ID)
	} else if action == "remove" {
		_, err = h.Pool.Exec(r.Context(),
			`DELETE FROM bookmarks WHERE user_id = $1 AND tutorial_id = $2`, userID, t.ID)
	} else {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/tutorials/"+strconv.FormatInt(t.ID, 10), http.StatusSeeOther)
}

// Review сохраняет отзыв (создаёт новый или обновляет свой)
func (h *Handler) Review(w http.ResponseWriter, r *http.Request) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	t, ok := h.loadTutorial(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192) // не принимаем огромные формы
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	// Проверяем данные
	rating, err := strconv.Atoi(r.FormValue("rating"))
	if err != nil || rating < 1 || rating > 5 {
		h.showTutorial(w, r, t, http.StatusBadRequest, "Izvēlieties vērtējumu no 1 līdz 5.")
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if len([]rune(body)) > 1000 {
		h.showTutorial(w, r, t, http.StatusBadRequest, "Atsauksme ir pārāk gara (maksimums 1000 rakstzīmes).")
		return
	}

	// Если отзыв уже есть, обновляем. Но если модератор его убрал,
	// запись не меняется (WHERE removed_at IS NULL).
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO reviews (user_id, tutorial_id, rating, body) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, tutorial_id) DO UPDATE
		 SET rating = EXCLUDED.rating, body = EXCLUDED.body, updated_at = now()
		 WHERE reviews.removed_at IS NULL`,
		userID, t.ID, rating, body)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/tutorials/"+strconv.FormatInt(t.ID, 10), http.StatusSeeOther)
}

// DeleteReview удаляет мой собственный отзыв
func (h *Handler) DeleteReview(w http.ResponseWriter, r *http.Request) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	t, ok := h.loadTutorial(w, r)
	if !ok {
		return
	}

	// Условие user_id = $1 гарантирует, что удалить можно только свой отзыв
	_, err := h.Pool.Exec(r.Context(),
		`DELETE FROM reviews WHERE user_id = $1 AND tutorial_id = $2 AND removed_at IS NULL`,
		userID, t.ID)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/tutorials/"+strconv.FormatInt(t.ID, 10), http.StatusSeeOther)
}

// BookmarkRow это одна закладка в списке
type BookmarkRow struct {
	ID     int64
	Title  string
	Source string
	Board  string
}

// BookmarksPage это данные страницы закладок
type BookmarksPage struct {
	Items []BookmarkRow
}

// Bookmarks показывает мои закладки (только для вошедших)
func (h *Handler) Bookmarks(w http.ResponseWriter, r *http.Request) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Показываем только туториалы с досок, которые сейчас доступны
	ids := h.allowedBoardIDs(r)

	rows, err := h.Pool.Query(r.Context(),
		`SELECT t.id, v.title, s.name, b.name
		 FROM bookmarks bm
		 JOIN tutorials t ON t.id = bm.tutorial_id
		 JOIN boards b ON b.id = t.board_id
		 JOIN sources s ON s.id = t.source_id
		 JOIN tutorial_versions v ON v.tutorial_id = t.id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 WHERE bm.user_id = $1 AND b.id = ANY($2)
		 ORDER BY bm.created_at DESC`, userID, ids)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data BookmarksPage
	for rows.Next() {
		var b BookmarkRow
		err := rows.Scan(&b.ID, &b.Title, &b.Source, &b.Board)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		data.Items = append(data.Items, b)
	}

	h.page(w, http.StatusOK, "bookmarks.html", data)
}
