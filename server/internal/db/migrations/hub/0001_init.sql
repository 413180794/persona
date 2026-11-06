-- The hub's part of the first migration (the manager's is migrations/manager/0001_init.sql).

-- Devices that registered for push notifications (iPhone / Watch).
CREATE TABLE devices (
    device_id   text PRIMARY KEY,
    name        text NOT NULL DEFAULT '',
    kind        text NOT NULL DEFAULT '',
    apns_token  text NOT NULL DEFAULT '',
    apns_env    text NOT NULL DEFAULT '',
    topic       text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- The phone_* MCP tools the iPhone last announced, so Claude sees them while the phone is offline.
CREATE TABLE phone_tools (
    name          text PRIMARY KEY,
    description   text NOT NULL DEFAULT '',
    input_schema  jsonb NOT NULL DEFAULT '{}',
    position      int NOT NULL DEFAULT 0,
    updated_at    timestamptz NOT NULL DEFAULT now()
);
