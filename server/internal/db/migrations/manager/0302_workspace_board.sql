-- The personal workspace board is an independently versioned document, never session state.
CREATE TABLE IF NOT EXISTS workspace_board (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Last successful discovery for each machine remains visible while it is offline.
CREATE TABLE IF NOT EXISTS workspace_snapshots (
    machine_id text PRIMARY KEY,
    data jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
