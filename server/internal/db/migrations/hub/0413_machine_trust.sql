-- Whether the user said a computer is theirs (docs/assistant/onboarding.md 4.2 "Your computer <-> hub", #3347). No row is a confirmed
-- computer: every machine from before this, the ones /admin adds and the ones `hub pair --machine` adds. A pending one
-- gets no Persona-managed keys and runs nothing of the assistant's until it is confirmed, and is removed after a day.
CREATE TABLE IF NOT EXISTS machine_trust (
    machine_id   text PRIMARY KEY,
    state        text NOT NULL CHECK (state IN ('pending', 'confirmed')),
    added_by     text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    confirmed_at timestamptz
);

-- Who made a computer's pairing code, redeemed it, confirmed or rejected the computer, and when one timed out.
CREATE TABLE IF NOT EXISTS machine_audit (
    id         bigserial PRIMARY KEY,
    machine_id text NOT NULL,
    action     text NOT NULL,
    actor      text NOT NULL DEFAULT '',
    detail     jsonb NOT NULL DEFAULT '{}',
    at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS machine_audit_machine ON machine_audit (machine_id, at);
