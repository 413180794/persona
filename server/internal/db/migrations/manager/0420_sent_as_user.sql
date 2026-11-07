-- The ledger of what Persona wrote as the user through the connectors' write gate (docs/extensions/connectors.md "Sent as the
-- user", #5278): one row per write that went out, never one that was refused or failed. Kept on the manager's side
-- (the gate runs there); rows older than hub.json's sent_ledger_days (90 by default) are deleted.
--   target       where it went, by connector (GitHub: {"ref": "owner/name#12"}, mail: {"to": [...], "subject": ...})
--   text         what was written, cut to 4000 characters
--   summary      one line saying what it was
--   origin       the conversation that asked: {"machine_id", "session_id"} (empty for a request that names none)
--   let_through  how the gate let it through: {"kind": "rule", "rule": {...}} or
--                {"kind": "card", "interaction_id", "answered_by"?}
--   message_ref  the sent message's id on the other side, when the connector gave one ('' when it did not)
--   recalled_at  when the user took it back, if they did (set by the recall, extmgr/sent_recall.go)
CREATE TABLE IF NOT EXISTS sent_as_user (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    connector   text NOT NULL,
    action      text NOT NULL,
    target      jsonb NOT NULL DEFAULT '{}',
    text        text NOT NULL DEFAULT '',
    summary     text NOT NULL DEFAULT '',
    origin      jsonb NOT NULL DEFAULT '{}',
    let_through jsonb NOT NULL DEFAULT '{}',
    message_ref text NOT NULL DEFAULT '',
    recalled_at timestamptz
);
CREATE INDEX IF NOT EXISTS sent_as_user_at ON sent_as_user (at);
