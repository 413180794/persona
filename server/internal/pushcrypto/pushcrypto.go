// Package pushcrypto seals push payloads to a phone's public key (docs/operations/push-relay.md): HPKE base mode,
// DHKEM(P-256, HKDF-SHA256) + HKDF-SHA256 + AES-256-GCM. The iOS app opens them with CryptoKit's
// HPKE.P256_SHA256_AES_GCM_256 (apple/Shared/PushCrypto.swift); the relay in between only ever sees the result.
package pushcrypto

import (
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/nwverse/persona/server/internal/compat"
)

// Info binds a sealed payload to this use; both sides pass the same bytes.
const Info = compat.PushInfo
const cutoverInfo = "persona push v1"

// EncLen is the encapsulated key's length (an uncompressed P-256 point) at the start of a sealed payload; MinSealed is
// the shortest a sealed payload can be (that plus the AES-GCM tag, for an empty plaintext).
const (
	EncLen    = 65
	MinSealed = EncLen + 16
)

// PublicKey decodes a phone's key as the app sends it: the base64 of the X9.63 uncompressed point (65 bytes).
func PublicKey(b64 string) (*ecdh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("push key: %w", err)
	}
	k, err := ecdh.P256().NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("push key: %w", err)
	}
	return k, nil
}

// Seal encrypts plaintext to pub. The result is enc (65 bytes) followed by the AEAD output.
func Seal(pub *ecdh.PublicKey, plaintext []byte) ([]byte, error) {
	if pub == nil {
		return nil, errors.New("push key: missing")
	}
	pk, err := hpke.NewDHKEMPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return hpke.Seal(pk, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(compat.PushInfo), plaintext)
}

// Open is Seal's inverse (tests, and the relay's end-to-end script).
func Open(priv *ecdh.PrivateKey, sealed []byte) ([]byte, error) {
	k, err := hpke.NewDHKEMPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	plain, err := hpke.Open(k, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(compat.PushInfo), sealed)
	if err == nil {
		return plain, nil
	}
	// Already-produced cutover envelopes remain readable, without changing keys.
	return hpke.Open(k, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(cutoverInfo), sealed)
}
