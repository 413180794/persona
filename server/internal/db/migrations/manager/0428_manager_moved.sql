-- The default manager's move into a database of its own (docs/server/server.md "The default manager's own database"). In its
-- own database, 'in': the copy that filled it went through, in the same transaction as the data. In the database it
-- shared with the hub, 'out': its data left from there. peer is the other database (its server's system identifier
-- and its name, no credentials). Nothing else reads it.
CREATE TABLE IF NOT EXISTS manager_moved (
    direction text PRIMARY KEY CHECK (direction IN ('in', 'out')),
    at        timestamptz NOT NULL DEFAULT now(),
    peer      text NOT NULL DEFAULT ''
);
