package update

import (
	"crypto/ed25519"
	"encoding/base64"
)

// trustedKeys are the publisher's public keys, key id -> base64 of the key: a hub installs only a release signed with
// one of them. `jarvisd release keygen` prints the line to add (docs/hub-update.md「发布者生成钥匙」). Empty until
// the publisher makes one: a hub then only says a newer release is out and installs nothing.
var trustedKeys = map[string]string{}

// TrustedKeys is trustedKeys decoded (a malformed entry is skipped: it could never verify anything).
func TrustedKeys() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for id, b64 := range trustedKeys {
		k, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(k) != ed25519.PublicKeySize || KeyID(k) != id {
			continue
		}
		out = append(out, ed25519.PublicKey(k))
	}
	return out
}
