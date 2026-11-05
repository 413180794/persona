-- Hub startup (ExpireStaleInteractions) looks up the pending interactions; only a handful are pending at a time while
-- the table keeps every past one, so without this the lookup scans the whole history on each start.
CREATE INDEX IF NOT EXISTS interactions_pending ON interactions (id) WHERE status = 'pending';
