-- Pushing through the relay (docs/push-relay.md): the phone's public key (payloads are sealed to it), the relay it
-- registered with and the grant the relay gave it. Empty for a hub that pushes to APNs directly.
-- (Numbered 0081 to stay clear of other lanes' numbers.)
ALTER TABLE devices ADD COLUMN IF NOT EXISTS push_key text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS relay_url text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS relay_grant text NOT NULL DEFAULT '';
