-- Why the phone could not register with its push relay, in its own words ("403 topic not served", "301 <new address>",
-- a network error): with no grant and no apns.json the hub cannot push to it, and Settings › Devices and `hub doctor`
-- say why (docs/operations/push-relay.md "Which way a device is pushed", #5998). Empty when the registration went through.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS relay_error text NOT NULL DEFAULT '';
