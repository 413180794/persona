-- Which device credential (device_tokens.id) registered a device for pushes: revoking the credential (退出那台 Mac,
-- 设置 › 设备) forgets the device, so a signed-out app stops getting notifications. Empty for the pairing
-- token and rows from before. (Numbered 0181 to stay clear of other lanes' numbers.)
ALTER TABLE devices ADD COLUMN IF NOT EXISTS credential text NOT NULL DEFAULT '';
