-- NodeSessions (node state.load) reads a machine's sessions from the last 60 days, but node_sessions is never pruned and
-- its primary key only narrows to the machine, so every session the machine ever had was read and filtered on the heap.
CREATE INDEX IF NOT EXISTS node_sessions_started ON node_sessions (machine_id, started_at);
