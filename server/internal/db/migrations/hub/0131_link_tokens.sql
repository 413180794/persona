-- persona-link (hub/link.go): a computer that dials the hub instead of being reached over ssh. Its credential is a
-- device token of kind link, bound to one machine of hub.json, redeemed from a pairing code made for that machine.
ALTER TABLE device_tokens ADD COLUMN IF NOT EXISTS machine_id text;
ALTER TABLE pair_tokens ADD COLUMN IF NOT EXISTS machine_id text;
