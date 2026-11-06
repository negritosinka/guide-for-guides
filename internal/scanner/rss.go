package scanner

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Так сайт видит нашего робота
const userAgent = "GuideForGuidesBot/1.0 (studentu projekts)"

// Структуры под формат RSS. Поля, которых тут нет, Go просто пропускает.
type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	Categories []string `xml:"category"`
}

// fetchRSS скачивает ленту по адресу и разбирает её на записи
func fetchRSS(feedURL string) ([]rssItem, error) {
	// Таймаут: не ждём ответа дольше 20 секунд
	client := &http.Client{Timeout: 20 * time.Second}

	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сайт ответил статусом %d", resp.StatusCode)
	}

	// Читаем не больше 10 МБ, чтобы огромный ответ не забил память
	body := io.LimitReader(resp.Body, 10*1024*1024)

	var feed rssFeed
	err = xml.NewDecoder(body).Decode(&feed)
	if err != nil {
		return nil, err
	}
	return feed.Items, nil
}

// hasCategory проверяет, есть ли категория в списке (без учёта регистра букв)
func hasCategory(categories []string, name string) bool {
	for _, c := range categories {
		if strings.EqualFold(strings.TrimSpace(c), name) {
			return true
		}
	}
	return false
}

// cleanCategories убирает пробелы и оставляет не больше 5 категорий
func cleanCategories(categories []string) []string {
	var result []string
	for _, c := range categories {
		c = strings.TrimSpace(c)
		if c != "" && len(result) < 5 {
			result = append(result, c)
		}
	}
	return result
}
