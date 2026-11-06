-- An assistant conversation's background tasks and monitors running right now (descriptions): after a node restart,
-- which kills them, the conversation is told what it lost (node/assistant.go recoverAssistantTasks).
ALTER TABLE node_sessions ADD COLUMN IF NOT EXISTS tasks text[] NOT NULL DEFAULT '{}';
