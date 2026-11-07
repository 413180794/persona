-- Todos and their agent links (hub/todos.go, docs/assistant/todos.md).
CREATE TABLE IF NOT EXISTS todos (
    id         text PRIMARY KEY,
    title      text NOT NULL,
    notes      text NOT NULL DEFAULT '',
    done_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS todo_agents (
    todo_id      text NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    machine_id   text NOT NULL,
    session_id   text NOT NULL,
    linked_at    timestamptz NOT NULL DEFAULT now(),
    released_at  timestamptz,
    title        text NOT NULL DEFAULT '',
    cwd          text NOT NULL DEFAULT '',
    agent        text NOT NULL DEFAULT '',
    start_error  text NOT NULL DEFAULT '',
    PRIMARY KEY (todo_id, machine_id, session_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS todo_agents_active ON todo_agents (machine_id, session_id) WHERE released_at IS NULL;
CREATE INDEX IF NOT EXISTS todos_created ON todos (created_at DESC);
CREATE INDEX IF NOT EXISTS todo_agents_todo ON todo_agents (todo_id);
