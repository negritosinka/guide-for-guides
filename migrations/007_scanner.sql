-- Сайты, с которых собираем туториалы
CREATE TABLE sources (
    id       BIGSERIAL PRIMARY KEY,
    slug     TEXT NOT NULL UNIQUE,
    name     TEXT NOT NULL,
    base_url TEXT NOT NULL
);

-- Один туториал = одна ссылка на одном сайте
CREATE TABLE tutorials (
    id         BIGSERIAL   PRIMARY KEY,
    source_id  BIGINT      NOT NULL REFERENCES sources(id),
    board_id   BIGINT      NOT NULL REFERENCES boards(id),
    url        TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, url)
);

-- История: каждое изменение названия или категорий это новая строка
CREATE TABLE tutorial_versions (
    id           BIGSERIAL   PRIMARY KEY,
    tutorial_id  BIGINT      NOT NULL REFERENCES tutorials(id) ON DELETE CASCADE,
    title        TEXT        NOT NULL,
    categories   TEXT[]      NOT NULL DEFAULT '{}',
    content_hash TEXT        NOT NULL,
    seen_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tutorial_versions_tutorial ON tutorial_versions (tutorial_id, id);

-- Журнал запусков сканера
CREATE TABLE scan_runs (
    id          BIGSERIAL   PRIMARY KEY,
    source_id   BIGINT      NOT NULL REFERENCES sources(id),
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    found       INT NOT NULL DEFAULT 0,
    added       INT NOT NULL DEFAULT 0,
    changed     INT NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT ''
);