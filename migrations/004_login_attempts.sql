-- Неудачные попытки входа. Email храним только в виде хеша.
CREATE TABLE login_attempts (
    id         BIGSERIAL   PRIMARY KEY,
    email_hash TEXT        NOT NULL,
    ip         TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX login_attempts_time ON login_attempts (created_at);