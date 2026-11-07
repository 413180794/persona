-- The hub admin UI (docs/operations/admin.md): one administrator password (argon2id, PHC string) and its browser sessions.
-- A session's cookie value is never stored, only its SHA-256; csrf is the token the page sends on every write.
CREATE TABLE IF NOT EXISTS admin_credentials (
    id            smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_sessions (
    id_hash    text PRIMARY KEY,
    csrf       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    source     text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS admin_sessions_expires ON admin_sessions (expires_at);
