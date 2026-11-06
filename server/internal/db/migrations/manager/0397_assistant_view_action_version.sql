-- #2019: the card version the user confirmed, and HTML bridge replies sharing the card's one selection and outbox.
-- version 0 marks rows committed before the version was recorded; they keep their old idempotency rules.
ALTER TABLE assistant_view_actions ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 0
    CHECK (version >= 0 AND version <= 9007199254740991);
ALTER TABLE assistant_view_actions ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'action'
    CHECK (kind IN ('action', 'html'));
ALTER TABLE assistant_view_actions ADD COLUMN IF NOT EXISTS reply text NOT NULL DEFAULT ''
    CHECK (char_length(reply) <= 2000);
