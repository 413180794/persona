-- When a message card's reply went out, as a column (body.sent_at stays the copy the clients read): the incremental
-- feed poll lists the cards sent after since through an index instead of reading every row of the conversation.
ALTER TABLE assistant_feed ADD COLUMN IF NOT EXISTS sent_at timestamptz;

UPDATE assistant_feed SET sent_at = (body->>'sent_at')::timestamptz
    WHERE kind = 'message' AND sent_at IS NULL AND body->>'sent_at' IS NOT NULL;

CREATE INDEX IF NOT EXISTS assistant_feed_sent ON assistant_feed (conversation, sent_at) WHERE kind = 'message';
