-- Which key the manager's secrets in this database are sealed with (internal/assistant keyrotate.go): the built-in
-- manager's secret.key started as a copy of the hub's and is rotated to one of its own once. One row, written in the
-- same transaction as the re-seal; none before the first rotation. fingerprint and previous are
-- secretkey.Fingerprint of the key and of the one it replaced (they do not tell the key); previous_dropped_at is when
-- the old key, kept for reading meanwhile, was deleted.
CREATE TABLE IF NOT EXISTS sealing_key (
    one                 boolean PRIMARY KEY DEFAULT true CHECK (one),
    fingerprint         text NOT NULL,
    previous            text NOT NULL DEFAULT '',
    rotated_at          timestamptz NOT NULL DEFAULT now(),
    previous_dropped_at timestamptz
);
