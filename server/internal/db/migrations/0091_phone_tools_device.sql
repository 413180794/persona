-- Keep existing registrations available until the legacy phone registers again.
ALTER TABLE phone_tools ADD COLUMN device_id text NOT NULL DEFAULT 'legacy';
ALTER TABLE phone_tools ADD COLUMN platform text NOT NULL DEFAULT '';
ALTER TABLE phone_tools ADD COLUMN credential_id text NOT NULL DEFAULT '';
ALTER TABLE phone_tools DROP CONSTRAINT phone_tools_pkey;
ALTER TABLE phone_tools ADD PRIMARY KEY (device_id, name);
CREATE INDEX phone_tools_credential ON phone_tools (credential_id);
-- Existing iOS push IDs differ from credential IDs; bind them when the app registers again.
ALTER TABLE devices ADD COLUMN credential_id text NOT NULL DEFAULT '';
