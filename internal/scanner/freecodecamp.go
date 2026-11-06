package scanner

import (
	"strings"
	"time"
)

// FreeCodeCampParser собирает туториалы с freeCodeCamp
type FreeCodeCampParser struct{}

func (p FreeCodeCampParser) Slug() string    { return "freecodecamp" }
func (p FreeCodeCampParser) Name() string    { return "freeCodeCamp" }
func (p FreeCodeCampParser) BaseURL() string { return "https://www.freecodecamp.org/news/" }

// Ленты, которые мы читаем. Чтобы добавить ещё одну, допиши адрес в список.
var freeCodeCampFeeds = []string{
	"https://www.freecodecamp.org/news/tag/learn-to-code/rss/",
}

func (p FreeCodeCampParser) Scrape() ([]Tutorial, error) {
	var result []Tutorial

	for _, feedURL := range freeCodeCampFeeds {
		items, err := fetchRSS(feedURL)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			title := strings.TrimSpace(item.Title)
			link := strings.TrimSpace(item.Link)

			// Пропускаем пустое и странные ссылки (нужен только https)
			if title == "" || !strings.HasPrefix(link, "https://") {
				continue
			}
			// Подкасты это не туториалы
			if hasCategory(item.Categories, "podcast") {
				continue
			}

			result = append(result, Tutorial{
				Title:      title,
				URL:        link,
				Board:      "programming", // все записи идут на доску Programmēšana
				Categories: cleanCategories(item.Categories),
			})
		}

		// Небольшая пауза между запросами к одному сайту
		time.Sleep(2 * time.Second)
	}

	return result, nil
}
