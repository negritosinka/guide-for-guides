-- Закладки: пара "пользователь + туториал"
CREATE TABLE bookmarks (
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tutorial_id BIGINT      NOT NULL REFERENCES tutorials(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, tutorial_id)
);

-- Отзывы: один отзыв от одного пользователя на один туториал
CREATE TABLE reviews (
    id          BIGSERIAL   PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tutorial_id BIGINT      NOT NULL REFERENCES tutorials(id) ON DELETE CASCADE,
    rating      INT         NOT NULL CHECK (rating BETWEEN 1 AND 5),
    body        TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    removed_at  TIMESTAMPTZ, -- если не NULL, отзыв убрал модератор
    UNIQUE (user_id, tutorial_id)
);