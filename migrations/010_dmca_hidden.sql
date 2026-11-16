-- Скрытый туториал: не показывается нигде, но остаётся в базе
ALTER TABLE tutorials ADD COLUMN hidden_at TIMESTAMPTZ;

-- Жалоба теперь может быть и на туториал
ALTER TABLE reports ADD COLUMN tutorial_id BIGINT REFERENCES tutorials(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX reports_one_open_tutorial ON reports (reporter_id, tutorial_id)
    WHERE status = 'open' AND tutorial_id IS NOT NULL;

-- DMCA-заявки от правообладателей (вход на сайт не нужен)
CREATE TABLE dmca_requests (
    id           BIGSERIAL   PRIMARY KEY,
    name         TEXT        NOT NULL,
    email        TEXT        NOT NULL,
    tutorial_id  BIGINT      NOT NULL REFERENCES tutorials(id),
    description  TEXT        NOT NULL,
    original_url TEXT        NOT NULL DEFAULT '',
    signature    TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'open'
                 CHECK (status IN ('open', 'accepted', 'rejected')),
    handled_by   BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    handled_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);