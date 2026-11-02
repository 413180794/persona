package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The publisher signs latest.json with an ed25519 key kept on their own machine (publish.signing_key, never in the
// repository); hubs trust the public keys compiled in (keys.go). docs/hub-update.md「版本清单和签名」.

var (
	ErrUnsigned     = errors.New("the release is not signed")
	ErrNoTrustedKey = errors.New("this jarvisd trusts no signing key yet")
	ErrUnknownKey   = errors.New("the release is signed with a key this jarvisd does not trust")
	ErrBadSignature = errors.New("the release's signature does not match it")
)

// Signature is latest.json.sig.
type Signature struct {
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"` // base64 of the ed25519 signature of latest.json's bytes
}

// KeyID names a public key: the first 16 hex digits of its SHA-256.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:16]
}

const keyFileHeader = "jarvis-release-signing-key v1"

// Keygen writes a new private key to path (0600; an existing file is never overwritten) and returns its public key.
func Keygen(path string) (ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s\n%s\n", keyFileHeader, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		return nil, err
	}
	return pub, f.Close()
}

// LoadPrivate reads a key Keygen wrote, refusing one others can read.
func LoadPrivate(path string) (ed25519.PrivateKey, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s can be read by others (chmod 600 it)", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || lines[0] != keyFileHeader {
		return nil, fmt.Errorf("%s is not a release signing key", path)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a release signing key", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Sign signs data.
func Sign(priv ed25519.PrivateKey, data []byte) Signature {
	pub, _ := priv.Public().(ed25519.PublicKey)
	return Signature{KeyID: KeyID(pub), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))}
}

// Verify checks that sig (latest.json.sig's bytes; empty: none served) signs data with one of keys.
func Verify(data, sig []byte, keys []ed25519.PublicKey) error {
	if len(keys) == 0 {
		return ErrNoTrustedKey
	}
	if len(strings.TrimSpace(string(sig))) == 0 {
		return ErrUnsigned
	}
	var s Signature
	if err := json.Unmarshal(sig, &s); err != nil {
		return fmt.Errorf("%w: %v", ErrBadSignature, err)
	}
	raw, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadSignature, err)
	}
	for _, k := range keys {
		if KeyID(k) == s.KeyID {
			if ed25519.Verify(k, data, raw) {
				return nil
			}
			return ErrBadSignature
		}
	}
	return ErrUnknownKey
}
