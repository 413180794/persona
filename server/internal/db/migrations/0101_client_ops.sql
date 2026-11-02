CREATE TABLE client_ops (
    id uuid PRIMARY KEY,
    device_id text NOT NULL DEFAULT '',
    kind text NOT NULL,
    target jsonb NOT NULL,
    digest text NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done', 'failed', 'unknown')),
    result jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX client_ops_created_at_idx ON client_ops (created_at);
