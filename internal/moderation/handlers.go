package moderation

import (
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"guide-for-guides/internal/auth"
)

type Handler struct {
	Pool *pgxpool.Pool
	Tmpl *template.Template
	Auth *auth.Handler // нужен, чтобы узнать, кто вошёл
}

// MessageData для страницы с коротким сообщением
type MessageData struct {
	Title string
	Text  string
}

// Причины жалоб: ключ из формы и текст, который увидит модератор
var reasons = map[string]string{
	"spam":     "Surogātpasts vai reklāma",
	"abuse":    "Aizvainojošs saturs",
	"offtopic": "Nav par tēmu",
	"other":    "Cits iemesls",
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

// needLogin возвращает id вошедшего пользователя.
// Если пользователь не вошёл, отправляет его на страницу входа.
func (h *Handler) needLogin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, _, loggedIn := h.Auth.CurrentUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return 0, false
	}
	return userID, true
}

// tooManyOpen защищает от спама: не больше 10 неразобранных жалоб от одного человека
func (h *Handler) tooManyOpen(r *http.Request, userID int64) bool {
	var count int
	err := h.Pool.QueryRow(r.Context(),
		`SELECT count(*) FROM reports WHERE reporter_id = $1 AND status = 'open'`,
		userID).Scan(&count)
	if err != nil {
		log.Println("ошибка базы:", err)
		return false
	}
	return count >= 10
}

// ---------- Жалоба на отзыв ----------

type ReportData struct {
	ReviewID   int64
	TutorialID int64
	Error      string
}

// findReview проверяет, что отзыв существует, не убран и написан не мной.
// Возвращает id отзыва, id туториала и true, если жаловаться можно.
func (h *Handler) findReview(r *http.Request, userID int64) (int64, int64, bool) {
	reviewID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, 0, false
	}

	var authorID, tutorialID int64
	err = h.Pool.QueryRow(r.Context(),
		`SELECT user_id, tutorial_id FROM reviews WHERE id = $1 AND removed_at IS NULL`,
		reviewID).Scan(&authorID, &tutorialID)
	if err != nil || authorID == userID {
		return 0, 0, false
	}
	return reviewID, tutorialID, true
}

// ReportForm показывает форму жалобы
func (h *Handler) ReportForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.needLogin(w, r)
	if !ok {
		return
	}
	reviewID, tutorialID, ok := h.findReview(r, userID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.page(w, http.StatusOK, "report.html", ReportData{ReviewID: reviewID, TutorialID: tutorialID})
}

// ReportSubmit сохраняет жалобу
func (h *Handler) ReportSubmit(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.needLogin(w, r)
	if !ok {
		return
	}
	reviewID, tutorialID, ok := h.findReview(r, userID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	form := ReportData{ReviewID: reviewID, TutorialID: tutorialID}

	// Причина должна быть из нашего списка
	reason, known := reasons[r.FormValue("reason")]
	if !known {
		form.Error = "Izvēlieties iemeslu."
		h.page(w, http.StatusBadRequest, "report.html", form)
		return
	}

	details := strings.TrimSpace(r.FormValue("details"))
	if len([]rune(details)) > 500 {
		form.Error = "Paskaidrojums ir pārāk garš (maksimums 500 rakstzīmes)."
		h.page(w, http.StatusBadRequest, "report.html", form)
		return
	}

	if h.tooManyOpen(r, userID) {
		h.message(w, http.StatusTooManyRequests, "Kļūda",
			"Jums jau ir daudz neizskatītu ziņojumu. Lūdzu, uzgaidiet.")
		return
	}

	// Если такая открытая жалоба от вас уже есть, вставка ничего не сделает
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO reports (kind, reporter_id, review_id, reason, details)
		 VALUES ('content', $1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		userID, reviewID, reason, details)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.message(w, http.StatusOK, "Paldies!", "Jūsu ziņojums ir saņemts. Moderatori to izskatīs.")
}

// ---------- Сообщение об ошибке на сайте ----------

type BugData struct {
	Error   string
	Details string
}

func (h *Handler) BugForm(w http.ResponseWriter, r *http.Request) {
	_, ok := h.needLogin(w, r)
	if !ok {
		return
	}
	h.page(w, http.StatusOK, "bug.html", BugData{})
}

func (h *Handler) BugSubmit(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.needLogin(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	details := strings.TrimSpace(r.FormValue("details"))
	length := len([]rune(details))
	if length < 10 || length > 1000 {
		h.page(w, http.StatusBadRequest, "bug.html", BugData{
			Error:   "Aprakstam jābūt no 10 līdz 1000 rakstzīmēm.",
			Details: details,
		})
		return
	}

	if h.tooManyOpen(r, userID) {
		h.message(w, http.StatusTooManyRequests, "Kļūda",
			"Jums jau ir daudz neizskatītu ziņojumu. Lūdzu, uzgaidiet.")
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO reports (kind, reporter_id, reason, details)
		 VALUES ('bug', $1, 'Kļūda vietnē', $2)`, userID, details)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.message(w, http.StatusOK, "Paldies!", "Jūsu ziņojums par kļūdu ir saņemts.")
}

// ---------- Мои убранные отзывы и апелляции ----------

type RemovedRow struct {
	ReviewID int64
	Title    string
	Date     string
	Status   string // "" (апелляции нет), "open" или "rejected"
}

type RemovedPage struct {
	Items []RemovedRow
}

// Removed показывает мои убранные отзывы
func (h *Handler) Removed(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.needLogin(w, r)
	if !ok {
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT r.id, v.title, r.removed_at,
		        COALESCE((SELECT a.status FROM appeals a WHERE a.review_id = r.id), '')
		 FROM reviews r
		 JOIN tutorial_versions v ON v.tutorial_id = r.tutorial_id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = r.tutorial_id)
		 WHERE r.user_id = $1 AND r.removed_at IS NOT NULL
		 ORDER BY r.removed_at DESC`, userID)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data RemovedPage
	for rows.Next() {
		var item RemovedRow
		var removed time.Time
		err := rows.Scan(&item.ReviewID, &item.Title, &removed, &item.Status)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		item.Date = removed.Format("2006-01-02")
		data.Items = append(data.Items, item)
	}

	h.page(w, http.StatusOK, "removed.html", data)
}

// AppealReview сохраняет апелляцию на убранный отзыв
func (h *Handler) AppealReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.needLogin(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	reviewID, err := strconv.ParseInt(r.FormValue("review_id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Апелляцию можно подать только на свой убранный отзыв
	var mine bool
	err = h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM reviews
		                WHERE id = $1 AND user_id = $2 AND removed_at IS NOT NULL)`,
		reviewID, userID).Scan(&mine)
	if err != nil || !mine {
		http.NotFound(w, r)
		return
	}

	text := strings.TrimSpace(r.FormValue("message"))
	length := len([]rune(text))
	if length < 10 || length > 1000 {
		h.message(w, http.StatusBadRequest, "Kļūda",
			"Apelācijas tekstam jābūt no 10 līdz 1000 rakstzīmēm.")
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO appeals (user_id, review_id, message) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`, userID, reviewID, text)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/my/removed", http.StatusSeeOther)
}

// ---------- Апелляция на блокировку аккаунта ----------

type BanAppealData struct {
	Error string
	Email string
}

func (h *Handler) BanAppealForm(w http.ResponseWriter, r *http.Request) {
	h.page(w, http.StatusOK, "appeal.html", BanAppealData{})
}

func (h *Handler) BanAppealSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	text := strings.TrimSpace(r.FormValue("message"))

	length := len([]rune(text))
	if length < 10 || length > 1000 {
		h.page(w, http.StatusBadRequest, "appeal.html", BanAppealData{
			Error: "Apelācijas tekstam jābūt no 10 līdz 1000 rakstzīmēm.",
			Email: email,
		})
		return
	}

	// Заблокированный пользователь не может войти, поэтому проверяем пароль здесь
	userID, ok := h.Auth.CheckLogin(r, email, password)
	if !ok {
		h.page(w, http.StatusUnauthorized, "appeal.html", BanAppealData{
			Error: "Nepareizs e-pasts vai parole (vai pārāk daudz mēģinājumu).",
			Email: email,
		})
		return
	}

	var banned bool
	err = h.Pool.QueryRow(r.Context(),
		`SELECT banned_at IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&banned)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	if !banned {
		h.message(w, http.StatusOK, "Apelācija nav vajadzīga", "Jūsu konts nav bloķēts.")
		return
	}

	// Если открытая апелляция уже есть, вставка ничего не сделает
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO appeals (user_id, message) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, text)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.message(w, http.StatusOK, "Apelācija iesniegta",
		"Moderatori izskatīs jūsu apelāciju. Mēģiniet ieiet vēlāk.")
}
