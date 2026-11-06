-- Which pushes each device wants (hub/notify.go NotifyPrefs); NULL means the defaults. Kept apart from the columns the
-- app overwrites on every registration.
-- (Numbered 0021 so lanes working in parallel don't pick the same version: versions are integers, gaps are fine.)
ALTER TABLE devices ADD COLUMN IF NOT EXISTS prefs jsonb;
