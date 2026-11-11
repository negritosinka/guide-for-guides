package boards

import (
	"log"
	"net/http"
	"net/url"
	"strings"
)

// filters хранит всё, что пользователь выбрал на странице поиска
type filters struct {
	Q        string // слова для поиска
	Board    string // slug выбранной доски
	Source   string // slug выбранного сайта
	Category string // выбранная категория
	Sort     string // "new" или "name"
}

// link собирает адрес страницы поиска с этими фильтрами
func (f filters) link() string {
	v := url.Values{}
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	if f.Board != "" {
		v.Set("board", f.Board)
	}
	if f.Source != "" {
		v.Set("source", f.Source)
	}
	if f.Category != "" {
		v.Set("category", f.Category)
	}
	if f.Sort == "name" {
		v.Set("sort", "name")
	}
	return "/search?" + v.Encode()
}

// FacetItem это одна строка в списке фильтров или сортировки
type FacetItem struct {
	Name   string
	Count  int
	Link   string // куда ведёт ссылка
	Active bool   // выбрано ли это сейчас
}

// SearchResult это один найденный туториал
type SearchResult struct {
	ID         int64
	Title      string
	URL        string
	Source     string
	Board      string
	Categories []string
}

// SearchPage это все данные для страницы поиска
type SearchPage struct {
	Query       string
	Total       int
	Results     []SearchResult
	BoardsF     []FacetItem
	SourcesF    []FacetItem
	CategoriesF []FacetItem
	SortLinks   []FacetItem
	HasFilters  bool
	ClearLink   string
}

// Общая часть всех запросов: последняя версия каждого туториала,
// только на разрешённых досках ($1) и с совпадением в названии ($2)
const searchFrom = `
	FROM tutorials t
	JOIN boards b ON b.id = t.board_id
	JOIN sources s ON s.id = t.source_id
	JOIN tutorial_versions v ON v.tutorial_id = t.id
	 AND v.id = (SELECT max(id) FROM tutorial_versions WHERE tutorial_id = t.id)
	WHERE b.id = ANY($1) AND v.title ILIKE $2`

// Выбранные фильтры: раздел ($3), сайт ($4), категория ($5)
const searchFilters = `
	AND ($3::text = '' OR b.slug = $3)
	AND ($4::text = '' OR s.slug = $4)
	AND ($5::text = '' OR $5::text = ANY(v.categories))`

// escapeLike убирает особый смысл у символов % и _ в запросе пользователя
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// allowedBoardIDs возвращает id досок, которые можно показать этому человеку
func (h *Handler) allowedBoardIDs(r *http.Request) []int64 {
	ids := []int64{}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, slug, name, description, status, COALESCE(required_role, '') FROM boards`)
	if err != nil {
		log.Println("ошибка базы:", err)
		return ids
	}
	defer rows.Close()

	roles := h.rolesOf(r)
	for rows.Next() {
		var id int64
		var b Board
		err := rows.Scan(&id, &b.Slug, &b.Name, &b.Description, &b.Status, &b.RequiredRole)
		if err != nil {
			continue
		}
		// Та же функция, что решает доступ на странице доски
		if access(b, roles) == "ok" {
			ids = append(ids, id)
		}
	}
	return ids
}

// facetRow это одно значение фильтра с количеством результатов
type facetRow struct {
	Key   string
	Name  string
	Count int
}

// facetRows выполняет запрос, который возвращает (ключ, название, количество)
func (h *Handler) facetRows(r *http.Request, sql string, ids []int64, pattern string) []facetRow {
	rows, err := h.Pool.Query(r.Context(), sql, ids, pattern)
	if err != nil {
		log.Println("ошибка базы:", err)
		return nil
	}
	defer rows.Close()

	var list []facetRow
	for rows.Next() {
		var fr facetRow
		err := rows.Scan(&fr.Key, &fr.Name, &fr.Count)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		list = append(list, fr)
	}
	return list
}

// makeItems превращает значения фильтра в строки со ссылками.
// kind говорит, какой это фильтр: "board", "source" или "category".
func makeItems(rows []facetRow, f filters, kind string) []FacetItem {
	var items []FacetItem

	for _, row := range rows {
		next := f // копия фильтров: меняем копию, а не оригинал
		active := false

		if kind == "board" {
			active = f.Board == row.Key
			next.Board = row.Key
			if active {
				next.Board = "" // повторный клик снимает фильтр
			}
		} else if kind == "source" {
			active = f.Source == row.Key
			next.Source = row.Key
			if active {
				next.Source = ""
			}
		} else {
			active = f.Category == row.Key
			next.Category = row.Key
			if active {
				next.Category = ""
			}
		}

		items = append(items, FacetItem{
			Name:   row.Name,
			Count:  row.Count,
			Link:   next.link(),
			Active: active,
		})
	}
	return items
}

// Search показывает страницу поиска
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	f := filters{
		Q:        strings.TrimSpace(query.Get("q")),
		Board:    query.Get("board"),
		Source:   query.Get("source"),
		Category: query.Get("category"),
		Sort:     query.Get("sort"),
	}

	// Ограничиваем длину запроса (считаем символы, а не байты, из-за букв ā, ē...)
	runes := []rune(f.Q)
	if len(runes) > 100 {
		f.Q = string(runes[:100])
	}
	if f.Sort != "name" {
		f.Sort = "new"
	}

	ids := h.allowedBoardIDs(r)
	pattern := "%" + escapeLike(f.Q) + "%"

	data := SearchPage{Query: f.Q}

	// 1. Фильтры: считаем значения среди результатов поиска
	boardRows := h.facetRows(r,
		`SELECT b.slug, b.name, count(*) `+searchFrom+
			` GROUP BY b.slug, b.name ORDER BY count(*) DESC, b.name`, ids, pattern)
	sourceRows := h.facetRows(r,
		`SELECT s.slug, s.name, count(*) `+searchFrom+
			` GROUP BY s.slug, s.name ORDER BY count(*) DESC, s.name`, ids, pattern)
	categoryRows := h.facetRows(r,
		`SELECT cat, cat, count(*) FROM (SELECT unnest(v.categories) AS cat `+searchFrom+
			`) x GROUP BY cat ORDER BY count(*) DESC, cat LIMIT 15`, ids, pattern)

	data.BoardsF = makeItems(boardRows, f, "board")
	data.SourcesF = makeItems(sourceRows, f, "source")
	data.CategoriesF = makeItems(categoryRows, f, "category")

	// 2. Сортировка. Значение выбирается из фиксированного списка,
	// текст от пользователя в SQL не попадает.
	orderBy := "t.id DESC"
	if f.Sort == "name" {
		orderBy = "lower(v.title)"
	}

	// 3. Сколько всего найдено (с учётом выбранных фильтров)
	err := h.Pool.QueryRow(r.Context(),
		`SELECT count(*) `+searchFrom+searchFilters,
		ids, pattern, f.Board, f.Source, f.Category).Scan(&data.Total)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}

	// 4. Сами результаты (первые 50)
	rows, err := h.Pool.Query(r.Context(),
		`SELECT t.id, v.title, t.url, s.name, b.name, v.categories `+searchFrom+searchFilters+
			` ORDER BY `+orderBy+` LIMIT 50`,
		ids, pattern, f.Board, f.Source, f.Category)
	if err != nil {
		log.Println("ошибка базы:", err)
		http.Error(w, "Servera kļūda", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var res SearchResult
		err := rows.Scan(&res.ID, &res.Title, &res.URL, &res.Source, &res.Board, &res.Categories)
		if err != nil {
			log.Println("ошибка чтения строки:", err)
			continue
		}
		data.Results = append(data.Results, res)
	}

	// 5. Ссылки сортировки и сброса фильтров
	byNew := f
	byNew.Sort = "new"
	byName := f
	byName.Sort = "name"
	data.SortLinks = []FacetItem{
		{Name: "Jaunākie vispirms", Link: byNew.link(), Active: f.Sort == "new"},
		{Name: "Pēc nosaukuma (A-Z)", Link: byName.link(), Active: f.Sort == "name"},
	}

	data.HasFilters = f.Board != "" || f.Source != "" || f.Category != ""
	onlyQuery := filters{Q: f.Q, Sort: f.Sort}
	data.ClearLink = onlyQuery.link()

	h.page(w, http.StatusOK, "search.html", data)
}
