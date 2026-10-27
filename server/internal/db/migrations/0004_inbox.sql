-- The unified inbox (hub/inbox.go): one row per "thing" — new messages in a Feishu chat, a mail thread — with its
-- AI triage and whether the user has dealt with it. The item itself is the JSON the hub works with.
CREATE TABLE inbox_items (
    id          text PRIMARY KEY,
    data        jsonb NOT NULL,
    updated_at  timestamptz NOT NULL,
    done_at     timestamptz
);
CREATE INDEX inbox_items_open ON inbox_items (done_at, updated_at DESC);
