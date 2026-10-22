CREATE TABLE users (
    id                BIGSERIAL PRIMARY KEY,
    email             TEXT        NOT NULL,
    password_hash     TEXT        NOT NULL,
    email_verified_at TIMESTAMPTZ,
    banned_at         TIMESTAMPTZ,
    flags_count       INT         NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Один email = один аккаунт, без учёта регистра букв
CREATE UNIQUE INDEX users_email_unique ON users (lower(email));

-- Роли это дополнительные привилегии. Обычный зарегистрированный
-- пользователь это просто строка в users без ролей.
CREATE TABLE roles (
    name TEXT PRIMARY KEY
);

INSERT INTO roles (name) VALUES ('moderator'), ('admin');

CREATE TABLE user_roles (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role    TEXT   NOT NULL REFERENCES roles(name),
    PRIMARY KEY (user_id, role)
);