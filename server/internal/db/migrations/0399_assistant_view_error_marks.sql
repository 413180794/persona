-- The latest time a reply of an assistant conversation drew only valid views: problems of replies written before it
-- were redone since, even when they are recorded after it (replies are registered concurrently, in any order).
CREATE TABLE IF NOT EXISTS assistant_view_error_marks (
    conversation text NOT NULL,
    machine_id   text NOT NULL,
    passed_at    timestamptz NOT NULL,
    PRIMARY KEY (conversation, machine_id)
);
