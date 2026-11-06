CREATE TABLE device_sessions (
    id bigserial PRIMARY KEY,
    device_id text NOT NULL CHECK (device_id <> ''),
    connected_at timestamptz NOT NULL,
    disconnected_at timestamptz,
    reason text CHECK (reason IN ('pong_timeout', 'closed', 'replaced', 'hub_stopped', 'error')),
    source text NOT NULL,
    heartbeat_interval integer NOT NULL CHECK (heartbeat_interval > 0),
    CHECK ((disconnected_at IS NULL) = (reason IS NULL)),
    CHECK (disconnected_at >= connected_at)
);

CREATE INDEX device_sessions_device_time ON device_sessions (device_id, connected_at);
CREATE INDEX device_sessions_disconnected ON device_sessions (disconnected_at);
