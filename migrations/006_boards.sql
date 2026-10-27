CREATE TABLE boards (
    id            BIGSERIAL   PRIMARY KEY,
    slug          TEXT        NOT NULL UNIQUE,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    status        TEXT        NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active', 'maintenance', 'disabled')),
    required_role TEXT        REFERENCES roles(name),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Какая доска использует какую
CREATE TABLE board_dependencies (
    board_id      BIGINT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    depends_on_id BIGINT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    PRIMARY KEY (board_id, depends_on_id)
);