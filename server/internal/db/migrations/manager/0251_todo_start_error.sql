-- A task whose agent did not start ends there (hub/tasks.go startPlanTask, #1077): done_at is set and start_error says
-- why, so nothing counts it as going on and the app can show it failed. Reopening a todo clears it.
ALTER TABLE todos
    ADD COLUMN IF NOT EXISTS start_error text NOT NULL DEFAULT '';
