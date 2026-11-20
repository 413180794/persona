-- A terminal receipt for each ledger write tracked by the local admission gate.
-- Keep it independently of the ledger's content retention so a lost reply
-- can be reconciled using the exact operation ID, even after pruning.
CREATE TABLE IF NOT EXISTS sent_record_operations (
    operation_id text PRIMARY KEY CHECK (operation_id ~ '^[0-9a-f]{32}$')
);
