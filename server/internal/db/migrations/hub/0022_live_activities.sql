-- The lock screen / Dynamic Island "Fleet" activity (hub/liveactivity.go): one per device. push_token targets the running
-- activity (rotates; empty when none), push_to_start_token lets the hub start one while the app is not running.
CREATE TABLE IF NOT EXISTS live_activities (
    device_id           text PRIMARY KEY,
    activity_id         text NOT NULL DEFAULT '',
    push_token          text NOT NULL DEFAULT '',
    push_to_start_token text NOT NULL DEFAULT '',
    started_at          timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now()
);
