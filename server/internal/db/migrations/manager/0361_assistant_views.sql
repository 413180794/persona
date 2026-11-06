-- Live cards belong to one assistant conversation. Their feed position never changes when the card is updated.
CREATE TABLE IF NOT EXISTS assistant_views (
    id           text PRIMARY KEY,
    conversation text NOT NULL,
    machine_id   text NOT NULL,
    view         jsonb NOT NULL CHECK (jsonb_typeof(view) = 'object'),
    version      bigint NOT NULL DEFAULT 1 CHECK (version > 0 AND version <= 9007199254740991),
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS assistant_views_conversation ON assistant_views (conversation, created_at, id);
