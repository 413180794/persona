-- The manager's part of the first migration (the hub's is migrations/hub/0001_init.sql).

-- Sessions Persona started on each machine: launcher, directory and the herdr pane they run in.
CREATE TABLE node_sessions (
    machine_id  text NOT NULL,
    session_id  text NOT NULL,
    pane        text NOT NULL DEFAULT '',
    launcher    text NOT NULL DEFAULT '',
    cwd         text NOT NULL DEFAULT '',
    started_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (machine_id, session_id)
);

-- Mail accounts. data holds the non-secret fields as JSON; secrets are AES-GCM ciphertext made by the mail
-- package with ~/.persona/secret.key, which never enters the database.
CREATE TABLE mail_accounts (
    id            text PRIMARY KEY,
    email         text NOT NULL UNIQUE,
    data          jsonb NOT NULL,
    password_enc  text NOT NULL DEFAULT '',
    oauth_enc     text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Every question / plan / permission / screen interaction and how it ended.
CREATE TABLE interactions (
    id           text PRIMARY KEY,
    machine_id   text NOT NULL,
    session_id   text NOT NULL DEFAULT '',
    kind         text NOT NULL,
    payload      jsonb NOT NULL,
    status       text NOT NULL DEFAULT 'pending', -- pending | resolved | dismissed | cancelled | expired
    resolution   jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    resolved_at  timestamptz
);
CREATE INDEX interactions_session ON interactions (machine_id, session_id, created_at DESC);
CREATE INDEX interactions_created ON interactions (created_at DESC);
