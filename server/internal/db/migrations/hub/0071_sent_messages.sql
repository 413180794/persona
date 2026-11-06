-- Messages sent from the phone, by the app's client_msg_id: a retry (the app resends what got no answer, with the same
-- id) is not typed again after the node reconnected or the hub restarted (hub/send.go). state: sending (the hub went
-- away before it knew), delivered, unknown (the connection broke while it was being typed). Kept a day.
CREATE TABLE IF NOT EXISTS sent_messages (
    client_msg_id text PRIMARY KEY,
    machine_id    text NOT NULL,
    session_id    text NOT NULL,
    state         text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sent_messages_created ON sent_messages (created_at);
