package privacy

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ScheduleCleanup запускает очистку сразу, а потом каждые every.
// Запускать через go.
func ScheduleCleanup(ctx context.Context, pool *pgxpool.Pool, every time.Duration) {
	RunCleanup(ctx, pool)

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			RunCleanup(ctx, pool)
		case <-ctx.Done():
			return
		}
	}
}

// clean выполняет один запрос на удаление и пишет в лог, сколько строк удалено
func clean(ctx context.Context, pool *pgxpool.Pool, name string, sql string) {
	tag, err := pool.Exec(ctx, sql)
	if err != nil {
		log.Println("ошибка очистки", name, ":", err)
		return
	}
	if tag.RowsAffected() > 0 {
		log.Printf("очистка %s: удалено %d", name, tag.RowsAffected())
	}
}

// RunCleanup удаляет всё, что хранится дольше срока из политики конфиденциальности
func RunCleanup(ctx context.Context, pool *pgxpool.Pool) {
	// Попытки входа: не дольше суток
	clean(ctx, pool, "попытки входа",
		`DELETE FROM login_attempts WHERE created_at < now() - interval '1 day'`)

	// Сессии с истёкшим сроком
	clean(ctx, pool, "сессии",
		`DELETE FROM sessions WHERE expires_at < now()`)

	// Токены из писем: использованные и просроченные
	clean(ctx, pool, "токены писем",
		`DELETE FROM email_tokens WHERE used_at IS NOT NULL OR expires_at < now()`)

	// Неподтверждённые аккаунты старше 7 дней (сотрудников не трогаем)
	clean(ctx, pool, "неподтверждённые аккаунты",
		`DELETE FROM users
		 WHERE email_verified_at IS NULL
		   AND created_at < now() - interval '7 days'
		   AND NOT EXISTS (SELECT 1 FROM user_roles WHERE user_id = users.id)`)

	// Обработанные жалобы: через год после решения
	clean(ctx, pool, "жалобы",
		`DELETE FROM reports WHERE status <> 'open' AND handled_at < now() - interval '1 year'`)

	// DMCA-заявки (там имя и email заявителя): через год после решения
	clean(ctx, pool, "DMCA-заявки",
		`DELETE FROM dmca_requests WHERE status <> 'open' AND handled_at < now() - interval '1 year'`)

	// Журнал действий персонала: 2 года
	clean(ctx, pool, "журнал персонала",
		`DELETE FROM staff_actions WHERE created_at < now() - interval '2 years'`)
}
