-- Журнал действий персонала: кто, что и когда
CREATE TABLE staff_actions (
    id          BIGSERIAL   PRIMARY KEY,
    actor_id    BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    action      TEXT        NOT NULL,
    target_type TEXT        NOT NULL,
    target_id   BIGINT,
    details     TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);