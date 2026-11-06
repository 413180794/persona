-- Hub startup (RecoverClientOps) marks the pending client operations unknown; only a few are pending at a time while
-- the table keeps 30 days of operations, so without this the update scans the whole table on each start.
CREATE INDEX IF NOT EXISTS client_ops_pending ON client_ops (id) WHERE state = 'pending';
