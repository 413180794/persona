-- Where a todo came from (#6146, docs/assistant/todos.md "Where it came from"): {channel, chat_id, message_id, no_report?,
-- reported_at?}, NULL while it came from nowhere in particular. The message a request was asked in, so the outcome can be
-- told there once the todo is done; reported_at is the moment it was (so a restart does not tell it again) and no_report
-- the owner's "don't report back". One value per todo, read and written as a unit, hence a jsonb column like todos.thread.
-- Not unique: several todos may come from one message. Deleting the todo deletes it with the row.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS source jsonb
    CONSTRAINT todos_source_keys CHECK (source IS NULL OR (source ? 'channel' AND source ? 'chat_id' AND source ? 'message_id'));
