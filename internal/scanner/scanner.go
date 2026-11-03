package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schedule запускает сканирование сразу, а потом каждые every.
// Работает в фоне (запускать через go).
func Schedule(ctx context.Context, pool *pgxpool.Pool, parsers []Parser, every time.Duration) {
	RunAll(ctx, pool, parsers)

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			RunAll(ctx, pool, parsers)
		case <-ctx.Done():
			return
		}
	}
}

// RunAll запускает каждый парсер по очереди
func RunAll(ctx context.Context, pool *pgxpool.Pool, parsers []Parser) {
	for _, p := range parsers {
		runOne(ctx, pool, p)
	}
}

// runOne обрабатывает один сайт
func runOne(ctx context.Context, pool *pgxpool.Pool, p Parser) {
	log.Println("сканирование:", p.Name())

	// 1. Записываем сайт в таблицу sources (если уже есть, обновляем название)
	var sourceID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO sources (slug, name, base_url) VALUES ($1, $2, $3)
		 ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, base_url = EXCLUDED.base_url
		 RETURNING id`,
		p.Slug(), p.Name(), p.BaseURL()).Scan(&sourceID)
	if err != nil {
		log.Println("ошибка sources:", err)
		return
	}

	// 2. Записываем начало запуска
	var runID int64
	err = pool.QueryRow(ctx,
		`INSERT INTO scan_runs (source_id) VALUES ($1) RETURNING id`,
		sourceID).Scan(&runID)
	if err != nil {
		log.Println("ошибка scan_runs:", err)
		return
	}

	// 3. Парсер получает список с сайта
	items, err := p.Scrape()
	if err != nil {
		finishRun(ctx, pool, runID, 0, 0, 0, err.Error())
		log.Println("ошибка парсера", p.Name(), ":", err)
		return
	}

	// 4. Сохраняем каждый туториал
	found, added, changed := 0, 0, 0
	for _, item := range items {
		found++
		result, err := saveTutorial(ctx, pool, sourceID, item)
		if err != nil {
			log.Println("не удалось сохранить", item.URL, ":", err)
			continue
		}
		if result == "new" {
			added++
		} else if result == "changed" {
			changed++
		}
	}

	finishRun(ctx, pool, runID, found, added, changed, "")
	log.Printf("готово %s: найдено %d, новых %d, изменённых %d", p.Name(), found, added, changed)
}

// saveTutorial сохраняет один туториал.
// Возвращает "new" (первый раз), "changed" (новая версия) или "same" (ничего не изменилось).
func saveTutorial(ctx context.Context, pool *pgxpool.Pool, sourceID int64, item Tutorial) (string, error) {
	// Находим доску по slug
	var boardID int64
	err := pool.QueryRow(ctx, `SELECT id FROM boards WHERE slug = $1`, item.Board).Scan(&boardID)
	if err != nil {
		return "", err
	}

	// Добавляем туториал. Если такая ссылка уже есть, ничего не происходит.
	_, err = pool.Exec(ctx,
		`INSERT INTO tutorials (source_id, board_id, url) VALUES ($1, $2, $3)
		 ON CONFLICT (source_id, url) DO NOTHING`,
		sourceID, boardID, item.URL)
	if err != nil {
		return "", err
	}

	// Берём его id
	var tutorialID int64
	err = pool.QueryRow(ctx,
		`SELECT id FROM tutorials WHERE source_id = $1 AND url = $2`,
		sourceID, item.URL).Scan(&tutorialID)
	if err != nil {
		return "", err
	}

	// Сравниваем хеш с последней сохранённой версией
	newHash := makeHash(item)
	var lastHash string
	err = pool.QueryRow(ctx,
		`SELECT content_hash FROM tutorial_versions
		 WHERE tutorial_id = $1 ORDER BY id DESC LIMIT 1`,
		tutorialID).Scan(&lastHash)

	isFirst := err == pgx.ErrNoRows
	if err != nil && !isFirst {
		return "", err
	}
	if err == nil && lastHash == newHash {
		return "same", nil // ничего не изменилось, ничего не пишем
	}

	// Добавляем новую версию. Старые версии остаются нетронутыми.
	_, err = pool.Exec(ctx,
		`INSERT INTO tutorial_versions (tutorial_id, title, categories, content_hash)
		 VALUES ($1, $2, $3, $4)`,
		tutorialID, item.Title, item.Categories, newHash)
	if err != nil {
		return "", err
	}

	if isFirst {
		return "new", nil
	}
	return "changed", nil
}

// makeHash делает короткий "отпечаток" названия и категорий.
// Если хотя бы одна буква изменилась, отпечаток будет другим.
func makeHash(item Tutorial) string {
	text := item.Title + "|" + strings.Join(item.Categories, ",")
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// finishRun дописывает в scan_runs результат запуска
func finishRun(ctx context.Context, pool *pgxpool.Pool, runID int64, found, added, changed int, errText string) {
	_, err := pool.Exec(ctx,
		`UPDATE scan_runs
		 SET finished_at = now(), found = $1, added = $2, changed = $3, error = $4
		 WHERE id = $5`,
		found, added, changed, errText, runID)
	if err != nil {
		log.Println("ошибка finishRun:", err)
	}
}
