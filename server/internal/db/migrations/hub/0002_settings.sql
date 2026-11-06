-- Small app-wide settings edited from the phone (e.g. the assistant's launcher).
CREATE TABLE IF NOT EXISTS settings (
    key         text PRIMARY KEY,
    value       jsonb NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
