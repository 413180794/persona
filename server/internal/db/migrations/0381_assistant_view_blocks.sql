-- Inline cards are immutable definitions from trusted assistant transcript items, not client-supplied views.
CREATE TABLE IF NOT EXISTS assistant_view_blocks (
    conversation text NOT NULL,
    key          text NOT NULL,
    machine_id   text NOT NULL,
    item_id      text NOT NULL,
    block_index  integer NOT NULL CHECK (block_index >= 0 AND block_index < 3),
    view         jsonb NOT NULL CHECK (jsonb_typeof(view) = 'object'),
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (conversation, key)
);

CREATE TABLE IF NOT EXISTS assistant_view_errors (
    conversation text NOT NULL,
    machine_id   text NOT NULL,
    item_id      text NOT NULL,
    errors       jsonb NOT NULL,
    reported     boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (conversation, machine_id, item_id)
);
