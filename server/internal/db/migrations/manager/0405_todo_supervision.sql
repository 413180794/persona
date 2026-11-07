-- Supervised tasks (#3124, docs/assistant/unified-entry.md "Supervision"): a task the main conversation handed out remembers that
-- conversation as its supervisor, what done means (done_when), how long it should take, and where its review stands.
-- Kept apart from todos: a todo made by hand or by another conversation has no row.
CREATE TABLE IF NOT EXISTS todo_supervision (
    todo_id        text PRIMARY KEY REFERENCES todos (id) ON DELETE CASCADE,
    supervisor     text NOT NULL,
    done_when      jsonb NOT NULL DEFAULT '[]'::jsonb,
    expect_minutes integer NOT NULL DEFAULT 0,
    state          text NOT NULL DEFAULT '',
    round          integer NOT NULL DEFAULT 0,
    reviews        jsonb NOT NULL DEFAULT '[]'::jsonb,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
