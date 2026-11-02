CREATE TABLE lock_tests (
    test_id text PRIMARY KEY,
    device_id text NOT NULL CHECK (device_id <> ''),
    device_session_id bigint REFERENCES device_sessions(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    due_at timestamptz NOT NULL,
    sent_at timestamptz,
    received_at timestamptz,
    notification_shown boolean,
    status text NOT NULL DEFAULT 'scheduled'
        CHECK (status IN ('scheduled', 'sent', 'received', 'unavailable', 'interrupted')),
    CHECK (due_at >= created_at),
    CHECK (received_at IS NULL OR sent_at IS NOT NULL)
);

CREATE INDEX lock_tests_due ON lock_tests (due_at) WHERE status = 'scheduled';
CREATE INDEX lock_tests_created ON lock_tests (created_at);
