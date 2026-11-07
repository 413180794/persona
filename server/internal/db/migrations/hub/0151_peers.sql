-- Hub mesh (hub/peer_*.go, docs/server/mesh.md): the hubs this one is paired with, and the one-time codes that pair them.
-- Each side keeps the credential the other issued to it (sealed with the hub's ~/.persona/secret.key, never in the
-- clear) and the id of the one it issued in return: a device token of kind and role peer whose machine_id is the
-- peer's hub_id (only its hash is kept, like every device token).
CREATE TABLE IF NOT EXISTS peers (
    hub_id        text PRIMARY KEY,
    name          text NOT NULL DEFAULT '',
    fp            text NOT NULL,
    endpoints     jsonb NOT NULL DEFAULT '[]',
    token         text NOT NULL,
    credential_id text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS peer_codes (
    code_hash  text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    hub_id     text
);
