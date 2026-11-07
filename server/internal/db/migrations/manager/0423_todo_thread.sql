-- The chat thread a todo is bound to (#5279, docs/assistant/todos.md "Chat thread"): {channel, chat_id, thread_id, title}, NULL
-- while the todo has none. One value per todo, read and written as a unit, so a jsonb column like todo_supervision's
-- reviews rather than four more columns on every todos read. A thread belongs to at most one todo: the unique index
-- is over the three fields that name it (the title is only a label); the check keeps a row without one of them (which
-- the index would read as NULLs, all different) from slipping past it. Deleting the todo deletes the binding with it.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS thread jsonb
    CONSTRAINT todos_thread_keys CHECK (thread IS NULL OR (thread ? 'channel' AND thread ? 'chat_id' AND thread ? 'thread_id'));

CREATE UNIQUE INDEX IF NOT EXISTS todos_thread ON todos ((thread ->> 'channel'), (thread ->> 'chat_id'), (thread ->> 'thread_id'))
    WHERE thread IS NOT NULL;
