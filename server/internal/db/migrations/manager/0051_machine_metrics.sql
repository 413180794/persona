-- Machine monitoring (docs/server/machines.md). machine_systems: what each machine is, from its node's last connection, so
-- the pages know an offline machine's OS after a hub restart. machine_metrics: the last 24 hours of samples: raw rows
-- (step 10, every 10 s) for the latest hour, rolled up into one row a minute (step 60) after that (hub/metrics.go).
CREATE TABLE IF NOT EXISTS machine_systems (
    machine_id text PRIMARY KEY,
    system     jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS machine_metrics (
    machine_id text        NOT NULL,
    ts         timestamptz NOT NULL,
    step       smallint    NOT NULL, -- seconds the row covers: 10 raw, 60 rolled up
    cpu        real,                 -- busy %, NULL when not measured
    load       real,
    mem_used   bigint      NOT NULL,
    mem_total  bigint      NOT NULL,
    swap_used  bigint      NOT NULL,
    swap_total bigint      NOT NULL,
    net_rx     real,                 -- bytes/s
    net_tx     real,
    disk_used  bigint      NOT NULL,
    disk_total bigint      NOT NULL,
    PRIMARY KEY (machine_id, ts, step)
);
