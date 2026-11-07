-- What managers handed the hub lately, by manager and item id (a push's notification id, an outbox item's): an item
-- that comes again (a manager that did not hear the answer, a push sent live that comes again from the manager's
-- outbox) is delivered once, also by the next hub process (docs/operations/hub-restart.md "The manager while the hub is away").
-- Pruned after a couple of hours.
CREATE TABLE IF NOT EXISTS handed_over (
    manager text NOT NULL,
    id      text NOT NULL,
    at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (manager, id)
);

CREATE INDEX IF NOT EXISTS handed_over_at ON handed_over (at);
