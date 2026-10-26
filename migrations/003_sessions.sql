-- Сессии: кто сейчас вошёл. Храним хеш токена, а не сам токен.
CREATE TABLE sessions (
    token_hash TEXT        PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);