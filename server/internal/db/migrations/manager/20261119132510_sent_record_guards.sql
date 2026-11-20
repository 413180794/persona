-- An accepted sent.record_once retains an opaque reference hash across ledger pruning.
-- Keep this guard with the Manager database. It contains no message text, target,
-- source reference, row ID, or timestamp and is independent of sent_ledger_days.
CREATE TABLE IF NOT EXISTS sent_record_guards (
    key bytea PRIMARY KEY CHECK (octet_length(key) = 32)
);
