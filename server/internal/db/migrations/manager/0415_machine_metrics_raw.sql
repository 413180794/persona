-- The per-minute rollup (rollupMetricsSQL in metrics.go) deletes the raw rows older than an hour. The primary key
-- (machine_id, ts, step) cannot seek by ts, so without this index the DELETE scans all 24 hours of rows every minute
-- to move a few. The index holds only the raw rows (about the latest hour); its WHERE must match RawMetricStep (10).
CREATE INDEX IF NOT EXISTS machine_metrics_raw ON machine_metrics (ts) WHERE step = 10;
