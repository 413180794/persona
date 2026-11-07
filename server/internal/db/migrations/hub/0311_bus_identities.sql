-- The bus (hub/bus.go, docs/protocol/protocol.md "Bus"): the services that may dial GET /api/v1/bus, one row each. role is
-- manager (a manager process: the assistant) for now; the column is there for the roles that come later. Only the
-- sha256 of the token is kept; a new token for an identity replaces its old one.
CREATE TABLE IF NOT EXISTS bus_identities (
    id         text PRIMARY KEY,
    role       text NOT NULL,
    name       text NOT NULL DEFAULT '',
    token_hash text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
