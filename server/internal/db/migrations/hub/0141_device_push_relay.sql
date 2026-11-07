-- Pushing through the relay (docs/operations/push-relay.md): the phone's public key (payloads are sealed to it), the relay it
-- registered with and the grant the relay gave it. Empty for a hub that pushes to APNs directly.
-- (Was 0081 on master, a number the device_sessions migration already had: a database that recorded it as 0081 has
-- the record moved here before migrating, see renumbered in db.go. Idempotent, so it may run on either.)
ALTER TABLE devices ADD COLUMN IF NOT EXISTS push_key text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS relay_url text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS relay_grant text NOT NULL DEFAULT '';
