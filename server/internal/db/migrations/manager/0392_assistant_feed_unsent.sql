-- MarkFeedSent finds the cards about an item that are not sent yet (kind, ref, at, NOT body ? 'sent_at') every time
-- an inbox reply goes out; without this it scanned the whole feed, which is never pruned. Only message cards ever get
-- sent_at, so a sent message card leaves the index but every other kind (plan, task, answered, receipt, routed, today,
-- view) stays in it: the index is not small, it is as large as the non-message part of the feed plus the unsent cards.
-- Lookups stay fast since they lead with (kind, ref, at).
CREATE INDEX IF NOT EXISTS assistant_feed_unsent ON assistant_feed (kind, ref, at) WHERE NOT body ? 'sent_at';
