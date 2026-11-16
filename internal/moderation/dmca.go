package moderation

import (
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
)

// DMCAData хранит всё, что показываем в форме (чтобы не стирать текст после ошибки)
type DMCAData struct {
	Error       string
	Done        bool
	Name        string
	Email       string
	TutorialID  string
	Description string
	OriginalURL string
	Signature   string
}

// DMCAForm показывает форму. Номер туториала можно передать в адресе: /dmca?tutorial=5
func (h *Handler) DMCAForm(w http.ResponseWriter, r *http.Request) {
	h.page(w, http.StatusOK, "dmca.html", DMCAData{TutorialID: r.URL.Query().Get("tutorial")})
}

// checkDMCA проверяет поля формы.
// Возвращает номер туториала и текст ошибки (пустой, если всё хорошо).
func checkDMCA(f DMCAData, goodFaith bool, accurate bool) (int64, string) {
	nameLen := len([]rune(f.Name))
	if nameLen < 2 || nameLen > 100 {
		return 0, "Ievadiet savu pilno vārdu (2 līdz 100 rakstzīmes)."
	}

	addr, err := mail.ParseAddress(f.Email)
	if err != nil || addr.Address != f.Email || len(f.Email) > 254 {
		return 0, "Ievadiet derīgu e-pasta adresi."
	}

	tutorialID, err := strconv.ParseInt(f.TutorialID, 10, 64)
	if err != nil || tutorialID < 1 {
		return 0, "Norādiet pamācības numuru."
	}

	descLen := len([]rune(f.Description))
	if descLen < 20 || descLen > 2000 {
		return 0, "Aprakstam jābūt no 20 līdz 2000 rakstzīmēm."
	}

	if f.OriginalURL != "" {
		isLink := strings.HasPrefix(f.OriginalURL, "http://") || strings.HasPrefix(f.OriginalURL, "https://")
		if !isLink || len(f.OriginalURL) > 300 {
			return 0, "Oriģinālā darba adresei jāsākas ar http:// vai https://."
		}
	}

	sigLen := len([]rune(f.Signature))
	if sigLen < 2 || sigLen > 100 {
		return 0, "Ierakstiet savu pilno vārdu kā parakstu."
	}

	if !goodFaith || !accurate {
		return 0, "Jāapstiprina abi apgalvojumi."
	}

	return tutorialID, ""
}

// DMCASubmit saglabā iesniegumu
func (h *Handler) DMCASubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Nederīgs pieprasījums", http.StatusBadRequest)
		return
	}

	// Ловушка для роботов: поле "website" скрыто, люди его не заполняют.
	// Если оно заполнено, делаем вид, что всё хорошо, но ничего не сохраняем.
	if r.FormValue("website") != "" {
		h.page(w, http.StatusOK, "dmca.html", DMCAData{Done: true})
		return
	}

	form := DMCAData{
		Name:        strings.TrimSpace(r.FormValue("name")),
		Email:       strings.TrimSpace(r.FormValue("email")),
		TutorialID:  strings.TrimSpace(r.FormValue("tutorial_id")),
		Description: strings.TrimSpace(r.FormValue("description")),
		OriginalURL: strings.TrimSpace(r.FormValue("original_url")),
		Signature:   strings.TrimSpace(r.FormValue("signature")),
	}

	tutorialID, errText := checkDMCA(form, r.FormValue("good_faith") == "on", r.FormValue("accurate") == "on")
	if errText != "" {
		form.Error = errText
		h.page(w, http.StatusBadRequest, "dmca.html", form)
		return
	}

	// Такой туториал должен существовать
	var exists bool
	err = h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM tutorials WHERE id = $1)`, tutorialID).Scan(&exists)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	if !exists {
		form.Error = "Pamācība ar šādu numuru nav atrasta."
		h.page(w, http.StatusBadRequest, "dmca.html", form)
		return
	}

	// Защита от спама: не больше 3 неразобранных заявок с одного email
	var open int
	err = h.Pool.QueryRow(r.Context(),
		`SELECT count(*) FROM dmca_requests WHERE lower(email) = lower($1) AND status = 'open'`,
		form.Email).Scan(&open)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	if open >= 3 {
		form.Error = "No šīs e-pasta adreses jau ir vairāki neizskatīti iesniegumi. Lūdzu, uzgaidiet."
		h.page(w, http.StatusTooManyRequests, "dmca.html", form)
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO dmca_requests (name, email, tutorial_id, description, original_url, signature)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		form.Name, form.Email, tutorialID, form.Description, form.OriginalURL, form.Signature)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	h.page(w, http.StatusOK, "dmca.html", DMCAData{Done: true})
}
