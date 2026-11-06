package extension

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// A market's signature (docs/extensions.md "Signatures", #1848): next to the index, <index>.sig is
// {"alg": "ed25519", "key_id": "<first 16 hex of sha256 of the 32-byte key>", "sig": "<base64 of the signature of the
// index file's raw bytes>"}. The hub trusts the key it saw first (or the one its owner accepted), never the index's own
// signing_key, which is only a hint of what a new key is.

// SigSuffix makes an index's name its signature's: persona-extensions.json.sig.
const SigSuffix = ".sig"

// Signature states of a market.
const (
	SigSigned     = "signed"      // the index verifies with the trusted key
	SigUnsigned   = "unsigned"    // no signature, and no key trusted yet: read as before
	SigInvalid    = "invalid"     // the signature is malformed, fails verification, or a trusted market stopped signing
	SigKeyChanged = "key_changed" // signed by another key than the trusted one
)

// sigFileMost bounds a .sig file.
const sigFileMost = 4 << 10

// MarketSignature is what the hub knows of a market's signature.
type MarketSignature struct {
	State string `json:"state"`
	// KeyID is the key the index is signed by (the trusted one when signed; the new one when key_changed).
	KeyID string `json:"key_id,omitempty"`
	// Pending is the new key the owner may accept (key_changed, when the index's signing_key is the key that signed
	// it), or "" with Unsign when the owner may accept a market that stopped signing.
	Pending string `json:"pending,omitempty"`
	Unsign  bool   `json:"unsign,omitempty"`
}

// Bad is whether the market's index is not to be used (keep the last good one, install nothing).
func (s MarketSignature) Bad() bool { return s.State == SigInvalid || s.State == SigKeyChanged }

// ParseSigningKey reads ed25519:<base64 of 32 bytes>.
func ParseSigningKey(s string) (ed25519.PublicKey, error) {
	b64, ok := strings.CutPrefix(strings.TrimSpace(s), "ed25519:")
	if !ok {
		return nil, errors.New("公钥要写成 ed25519:<base64>")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("公钥不是 32 字节的 ed25519 公钥")
	}
	return ed25519.PublicKey(raw), nil
}

// KeyID is a key's id: the first 16 hex of the sha256 of its 32 bytes.
func KeyID(k ed25519.PublicKey) string {
	sum := sha256.Sum256(k)
	return hex.EncodeToString(sum[:8])
}

type sigFile struct {
	Alg   string `json:"alg"`
	KeyID string `json:"key_id"`
	Sig   string `json:"sig"`
}

// verifies is whether sig (a .sig file) signs index with key.
func verifies(key ed25519.PublicKey, index []byte, sig sigFile) bool {
	raw, err := base64.StdEncoding.DecodeString(sig.Sig)
	return err == nil && sig.Alg == "ed25519" && sig.KeyID == KeyID(key) && ed25519.Verify(key, index, raw)
}

// CheckSignature judges an index (its raw bytes) by its .sig (nil: there is none) against the trusted key ("" when
// none is trusted yet). hint is the index's own signing_key. It returns the state, the key to trust from now on
// (trusted, or the one seen first), and why the index is refused (empty when it is not).
func CheckSignature(index, sig []byte, trusted, hint string) (MarketSignature, string, string) {
	var sf sigFile
	if sig != nil {
		if len(sig) > sigFileMost || json.Unmarshal(sig, &sf) != nil || sf.Alg != "ed25519" || sf.KeyID == "" {
			return MarketSignature{State: SigInvalid}, trusted, "市场的签名文件读不出来"
		}
	}
	hintKey, _ := ParseSigningKey(hint)
	if trusted == "" {
		if sig == nil {
			return MarketSignature{State: SigUnsigned}, "", ""
		}
		// Trust on first use: the key the index names, if it is the one that signed it.
		if hintKey != nil && verifies(hintKey, index, sf) {
			return MarketSignature{State: SigSigned, KeyID: sf.KeyID}, hint, ""
		}
		return MarketSignature{State: SigInvalid, KeyID: sf.KeyID}, "", "市场的签名对不上它自己写的公钥（signing_key）"
	}
	key, err := ParseSigningKey(trusted)
	if err != nil {
		return MarketSignature{State: SigInvalid}, trusted, "记下的公钥坏了：" + err.Error()
	}
	if sig == nil {
		return MarketSignature{State: SigInvalid, Unsign: true}, trusted,
			"市场以前签过名，现在没有签名了：确认是市场自己去掉的，再信任它（personad extension market trust）"
	}
	if sf.KeyID != KeyID(key) {
		s := MarketSignature{State: SigKeyChanged, KeyID: sf.KeyID}
		if hintKey != nil && verifies(hintKey, index, sf) {
			s.Pending = strings.TrimSpace(hint)
		}
		return s, trusted, "市场换了签名的 key（" + KeyID(key) + " → " + sf.KeyID +
			"）：确认是市场自己换的，再信任新 key（personad extension market trust）"
	}
	if !verifies(key, index, sf) {
		return MarketSignature{State: SigInvalid, KeyID: sf.KeyID}, trusted, "市场的签名对不上索引：索引可能被改过"
	}
	return MarketSignature{State: SigSigned, KeyID: sf.KeyID}, trusted, ""
}
