package scanner

// Tutorial это то, что парсер находит на сайте.
// Только базовая информация и ссылка на оригинал.
type Tutorial struct {
	Title      string
	URL        string // ссылка на оригинал
	Board      string // slug доски, например "cooking"
	Categories []string
}

// Parser описывает один сайт. Чтобы добавить новый сайт,
// нужно написать новый тип с этими четырьмя методами.
type Parser interface {
	Slug() string    // короткое имя, например "demo"
	Name() string    // название для людей
	BaseURL() string // адрес сайта
	Scrape() ([]Tutorial, error)
}
