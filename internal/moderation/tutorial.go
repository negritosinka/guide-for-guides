package moderation

import (
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Причины жалоб на туториалы
var tutorialReasons = map[string]string{
	"broken":   "Saite nedarbojas",
	"spam":     "Surogātpasts vai reklāma",
	"offtopic": "Zema kvalitāte vai nepareiza sadaļa",
	"other":    "Cits iemesls",
}

type TutorialReportData struct {
	TutorialID int64
	Title      string
	Error      string
}

// findTutorial проверяет, что туториал существует, не скрыт и его доска не выключена.
// Возвращает номер, название и true, если можно жаловаться.
func (h *Handler) findTutorial(r *http.Request) (int64, string, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, "", false
	}

	var title string
	err = h.Pool.QueryRow(r.Context(),
		`SELECT v.title FROM tutorials t
		 JOIN boards b ON b.id = t.board_id
		 JOIN tutorial_versions v ON v.tutorial_id = t.id
		  AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
		 WHERE t.id = $1 AND t.hidden_at IS NULL AND b.status <> 'disabled'`,
		id).Scan(&title)
	if err != nil {
		return 0, "", false
	}
	return id, title, true
}

// TutorialReportForm показывает форму жалобы на туториал
func (h *Handler) TutorialReportForm(w http.ResponseWriter, r *http.Request) {
	_, ok := h.needLogin(w, r)
	if !ok {
		return
	}
	id, title, ok := h.findTutorial(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.page(w, http.StatusOK, "report_tutorial.html", TutorialReportData{TutorialID: id, Title: title})
}

// TutorialReportSubmit сохраняет жалобу на туториал
func (h *Handler) TutorialReportSubmit(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.needLogin(w, r)
	if !ok {
		return
	}
	tutorialID, title, ok := h.findTutorial(r)
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

	form := TutorialReportData{TutorialID: tutorialID, Title: title}

	reason, known := tutorialReasons[r.FormValue("reason")]
	if !known {
		form.Error = "Izvēlieties iemeslu."
		h.page(w, http.StatusBadRequest, "report_tutorial.html", form)
		return
	}

	details := strings.TrimSpace(r.FormValue("details"))
	if len([]rune(details)) > 500 {
		form.Error = "Paskaidrojums ir pārāk garš (maksimums 500 rakstzīmes)."
		h.page(w, http.StatusBadRequest, "report_tutorial.html", form)
		return
	}

	if h.tooManyOpen(r, userID) {
		h.message(w, http.StatusTooManyRequests, "Kļūda",
			"Jums jau ir daudz neizskatītu ziņojumu. Lūdzu, uzgaidiet.")
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO reports (kind, reporter_id, tutorial_id, reason, details)
		 VALUES ('content', $1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		userID, tutorialID, reason, details)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.message(w, http.StatusOK, "Paldies!", "Jūsu ziņojums ir saņemts. Moderatori to izskatīs.")
}
