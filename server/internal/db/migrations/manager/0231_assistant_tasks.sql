-- Tasks dispatched from an assistant conversation (docs/assistant/unified-entry.md, #930): a task is a todo that remembers
-- which conversation opened it, plans waiting for the user, and the items the hub inserts into a conversation.
ALTER TABLE todos
    ADD COLUMN IF NOT EXISTS origin text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS plan_id text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS todos_origin ON todos (origin) WHERE origin <> '';

CREATE TABLE IF NOT EXISTS task_plans (
    id           text PRIMARY KEY,
    conversation text NOT NULL,
    tasks        jsonb NOT NULL,
    state        text NOT NULL DEFAULT 'open',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS task_plans_conversation ON task_plans (conversation, created_at);

CREATE TABLE IF NOT EXISTS assistant_feed (
    id           text PRIMARY KEY,
    conversation text NOT NULL,
    at           timestamptz NOT NULL,
    kind         text NOT NULL,
    ref          text NOT NULL DEFAULT '',
    body         jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS assistant_feed_conversation ON assistant_feed (conversation, at);
