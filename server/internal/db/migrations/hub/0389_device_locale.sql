-- The interface language a device registered with (docs/protocol/i18n.md): its pushes read in it; '' is the user's language.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS locale text NOT NULL DEFAULT '';
