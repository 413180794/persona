-- What the app on a device understands beyond the first version's pushes ("withdraw": a background push naming the
-- cards that are over, so their notifications leave the lock screen, #4349). The hub sends such pushes only to
-- devices that registered with the feature, so older apps get no extra background pushes.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS features text[] NOT NULL DEFAULT '{}';
