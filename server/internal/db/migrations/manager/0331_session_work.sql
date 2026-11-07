-- Where each agent session stands in its work (docs/assistant/agent-work.md, #1686): the hub triages a stopped session's turn
-- (working / blocked on a named precondition / done) and acts on it — sends "continue" after a transient model error, wakes
-- it when what it waits for should be over, asks the user on a card. One row per session, about its latest turn: the
-- hub's memory goes with every redeploy, so the retries, wake times and cards live here.
CREATE TABLE IF NOT EXISTS session_work (
    machine_id     text        NOT NULL,
    session_id     text        NOT NULL,
    turn_at        timestamptz NOT NULL, -- the end of the turn the row is about (the session's updated_at then)
    work           jsonb       NOT NULL, -- proto.WorkState
    wake_at        timestamptz,          -- when the hub acts next on its own; null when nothing is due
    retries        int         NOT NULL DEFAULT 0, -- "continue" sent for this run of transient errors
    wakes          int         NOT NULL DEFAULT 0, -- wakes sent for this wait
    interaction_id text        NOT NULL DEFAULT '', -- the decision card, while pending
    updated_at     timestamptz NOT NULL,
    PRIMARY KEY (machine_id, session_id)
);

-- What happened, for GET /api/v1/agents/work/stats: triaged, continued, woke, asked, answered, withdrawn, ended (a wait
-- ended: how long it took), sample (how many sessions were unread). Kept 30 days.
CREATE TABLE IF NOT EXISTS session_work_events (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL,
    machine_id text        NOT NULL DEFAULT '',
    session_id text        NOT NULL DEFAULT '',
    kind       text        NOT NULL,
    detail     jsonb       NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS session_work_events_at ON session_work_events (at);
