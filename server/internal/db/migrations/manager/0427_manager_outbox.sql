-- What a manager hands the hub while the hub is away (docs/operations/hub-restart.md "The manager while the hub is away"): the
-- pushes and the apps' live updates it decided on, kept in order (seq) until the hub took them. id is the item's own
-- (a push's notification id); the body is kept byte for byte (what the live call would have sent). Bounded by the
-- manager, each kind on its own (the oldest go first); nothing else reads it.
CREATE TABLE IF NOT EXISTS manager_outbox (
    seq        bigserial PRIMARY KEY,
    id         text NOT NULL UNIQUE,
    kind       text NOT NULL CHECK (kind IN ('push', 'broadcast')),
    body       bytea NOT NULL,
    created_at timestamptz NOT NULL
);
