package scanner

// DemoParser возвращает фиксированный список. Нужен только для проверки.
type DemoParser struct{}

func (p DemoParser) Slug() string    { return "demo" }
func (p DemoParser) Name() string    { return "Demo avots" }
func (p DemoParser) BaseURL() string { return "https://example.com" }

func (p DemoParser) Scrape() ([]Tutorial, error) {
	list := []Tutorial{
		{Title: "Kā pagatavot pankūkas (new)", URL: "https://example.com/pankukas",
			Board: "cooking", Categories: []string{"Brokastis", "Ātri"}},
		{Title: "Zupas pamati", URL: "https://example.com/zupas",
			Board: "cooking", Categories: []string{"Pusdienas"}},
		{Title: "Go valodas pamati", URL: "https://example.com/go-pamati",
			Board: "programming", Categories: []string{"Go", "Iesācējiem"}},
		{Title: "SQL vaicājumi", URL: "https://example.com/sql",
			Board: "programming", Categories: []string{"Datubāzes"}},
		{Title: "Krāsnes flīzēšana", URL: "https://example.com/flizes",
			Board: "home-repair", Categories: []string{"Flīzes"}},
		{Title: "Tomātu audzēšana", URL: "https://example.com/tomati",
			Board: "gardening", Categories: []string{"Dārzeņi"}},
		{Title: "LED shēma ar Arduino", URL: "https://example.com/arduino-led",
			Board: "electronics", Categories: []string{"Arduino", "Iesācējiem"}},
	}
	return list, nil
}
