-- Managers (docs/assistant/managers.md): every bus identity of role manager is a manager. The roster keeps where each one
-- said it runs on its last handshake and when it was last connected, so the apps can show an offline manager's
-- place and "last synced". manager_install_codes are the one-time codes a new manager trades for its bus token:
-- only their sha256 is kept, each is good for ten minutes and once.
ALTER TABLE bus_identities
    ADD COLUMN IF NOT EXISTS host       text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS os         text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS arch       text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS deploy     text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS version    text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_seen  timestamptz;

CREATE TABLE IF NOT EXISTS manager_install_codes (
    code_hash  text PRIMARY KEY,
    manager_id text NOT NULL,
    name       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
CREATE INDEX IF NOT EXISTS manager_install_codes_manager ON manager_install_codes (manager_id);
