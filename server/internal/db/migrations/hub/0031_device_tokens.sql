-- One credential per device (hub/devicetokens.go): the app trades the pairing token for its own, which can be
-- revoked alone. Only a hash is kept. (Numbered 0031 to stay clear of other lanes' numbers.)
CREATE TABLE IF NOT EXISTS device_tokens (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    kind       text NOT NULL,          -- phone | watch | other
    role       text NOT NULL,          -- full | answer
    token_hash text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz,
    revoked_at timestamptz
);
