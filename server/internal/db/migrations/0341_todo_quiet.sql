-- A task the main conversation opened while 设置 › 助手 › 主对话派的任务只在主对话里通知 (assistant.main_tasks_quiet)
-- was on is quiet (#1731, docs/unified-entry.md「安静的任务」): no pushes, Live Activity or task badges; the main
-- conversation is told instead. Decided once, when the todo is made: changing the setting leaves existing tasks.
ALTER TABLE todos
    ADD COLUMN IF NOT EXISTS quiet boolean NOT NULL DEFAULT false;
