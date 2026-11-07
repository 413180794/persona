-- Todo spaces and agent link sources (hub/todos.go, docs/assistant/todos.md, #762).
CREATE TABLE IF NOT EXISTS todo_spaces (
    todo_id    text NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    machine_id text NOT NULL,
    space      text NOT NULL,
    linked_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (todo_id, machine_id, space)
);

CREATE UNIQUE INDEX IF NOT EXISTS todo_spaces_unique_space ON todo_spaces (machine_id, space);
CREATE INDEX IF NOT EXISTS todo_spaces_todo ON todo_spaces (todo_id);

ALTER TABLE todo_agents
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS via text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS dismissed_at timestamptz;
