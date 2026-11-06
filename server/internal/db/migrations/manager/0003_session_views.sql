-- When the user last looked at a session in the app; a finished session is "unread" until then.
CREATE TABLE session_views (
    machine_id  text NOT NULL,
    session_id  text NOT NULL,
    seen_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (machine_id, session_id)
);
