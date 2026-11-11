-- Жалобы: на отзыв (content) или сообщение об ошибке на сайте (bug)
CREATE TABLE reports (
    id          BIGSERIAL   PRIMARY KEY,
    kind        TEXT        NOT NULL CHECK (kind IN ('content', 'bug')),
    reporter_id BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    review_id   BIGINT      REFERENCES reviews(id) ON DELETE CASCADE,
    reason      TEXT        NOT NULL,
    details     TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'done', 'dismissed')),
    handled_by  BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    handled_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Один человек может иметь только одну открытую жалобу на один отзыв
CREATE UNIQUE INDEX reports_one_open ON reports (reporter_id, review_id)
    WHERE status = 'open' AND review_id IS NOT NULL;

-- Флаги: каждое удаление отзыва даёт автору один флаг
CREATE TABLE user_flags (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    review_id  BIGINT      REFERENCES reviews(id) ON DELETE SET NULL,
    created_by BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Апелляции. Если review_id пустой, это апелляция на блокировку аккаунта.
CREATE TABLE appeals (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    review_id  BIGINT      REFERENCES reviews(id) ON DELETE CASCADE,
    message    TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'open'
               CHECK (status IN ('open', 'accepted', 'rejected')),
    handled_by BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    handled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- На один отзыв можно подать только одну апелляцию
CREATE UNIQUE INDEX appeals_one_per_review ON appeals (review_id)
    WHERE review_id IS NOT NULL;

-- Апелляция на блокировку: только одна открытая на пользователя
CREATE UNIQUE INDEX appeals_one_open_ban ON appeals (user_id)
    WHERE review_id IS NULL AND status = 'open';