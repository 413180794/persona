-- Pairing credentials survive hub restarts; their plaintext is never persisted.
CREATE TABLE IF NOT EXISTS pair_tokens (
    token_hash text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    device_id  text
);
