package boards

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Definition описывает одну доску
type Definition struct {
	Slug        string
	Name        string
	Description string
	Uses        []string // slug'и досок, которые эта доска использует
}

// All это список всех досок сайта.
// Чтобы добавить новую доску, достаточно добавить сюда одну запись.
var All = []Definition{
	{Slug: "cooking", Name: "Ēdiena gatavošana", Description: "Receptes un virtuves triki"},
	{Slug: "programming", Name: "Programmēšana", Description: "Programmēšanas valodas un rīki"},
	{Slug: "home-repair", Name: "Mājas remonts", Description: "Remonts un sīkie darbi mājās"},
	{Slug: "gardening", Name: "Dārzkopība", Description: "Dārzs, dārzeņi un puķes"},
	{Slug: "electronics", Name: "Elektronika", Description: "Elektronika pašdarināšanai",
		Uses: []string{"programming"}}, // пример зависимости: платы программируются
}

// Sync заносит доски в базу при запуске программы.
func Sync(ctx context.Context, pool *pgxpool.Pool) error {
	// 1. Добавляем доски. Если доска уже есть, обновляем только название и описание.
	// Статус и роль не трогаем, их выставляет админ.
	for _, b := range All {
		_, err := pool.Exec(ctx,
			`INSERT INTO boards (slug, name, description) VALUES ($1, $2, $3)
			 ON CONFLICT (slug) DO UPDATE
			 SET name = EXCLUDED.name, description = EXCLUDED.description`,
			b.Slug, b.Name, b.Description)
		if err != nil {
			return err
		}
	}

	// 2. Зависимости пересоздаём из списка каждый раз
	_, err := pool.Exec(ctx, `DELETE FROM board_dependencies`)
	if err != nil {
		return err
	}
	for _, b := range All {
		for _, used := range b.Uses {
			_, err := pool.Exec(ctx,
				`INSERT INTO board_dependencies (board_id, depends_on_id)
				 SELECT a.id, c.id FROM boards a, boards c
				 WHERE a.slug = $1 AND c.slug = $2
				 ON CONFLICT DO NOTHING`,
				b.Slug, used)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
