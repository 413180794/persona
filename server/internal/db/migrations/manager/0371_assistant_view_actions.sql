-- A card's reply is selected once. The same row is also the durable message outbox.
CREATE TABLE IF NOT EXISTS assistant_view_actions (
    conversation  text NOT NULL,
    key           text NOT NULL,
    action        text NOT NULL,
    page          text NOT NULL DEFAULT '',
    values        jsonb NOT NULL DEFAULT '{}'::jsonb,
    client_msg_id text NOT NULL,
    message_id    text NOT NULL UNIQUE,
    machine_id    text NOT NULL,
    text          text NOT NULL,
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    error         text NOT NULL DEFAULT '',
    revision      bigint NOT NULL DEFAULT 1 CHECK (revision > 0 AND revision <= 9007199254740991),
    claimed_until timestamptz,
    at            timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (conversation, key)
);

CREATE INDEX IF NOT EXISTS assistant_view_actions_pending ON assistant_view_actions (updated_at)
    WHERE status = 'pending';
