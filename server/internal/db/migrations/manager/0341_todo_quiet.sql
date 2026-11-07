-- A task the main conversation opened while Settings › Assistant › "Notify main-chat tasks only in the main chat" (assistant.main_tasks_quiet)
-- was on is quiet (#1731, docs/assistant/unified-entry.md "Quiet tasks"): no pushes, Live Activity or task badges; the main
-- conversation is told instead. Decided once, when the todo is made: changing the setting leaves existing tasks.
ALTER TABLE todos
    ADD COLUMN IF NOT EXISTS quiet boolean NOT NULL DEFAULT false;
