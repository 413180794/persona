-- Which device credential (device_tokens.id) registered a device for pushes: revoking the credential (signing out that Mac,
-- Settings › Devices) forgets the device, so a signed-out app stops getting notifications. Empty for the pairing
-- token and rows from before. (Numbered 0181 to stay clear of other lanes' numbers.)
ALTER TABLE devices ADD COLUMN IF NOT EXISTS credential text NOT NULL DEFAULT '';
