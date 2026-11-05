package extension

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// testKey is a market key: its signing_key text and a signer of index bytes.
func testKey(t *testing.T) (string, func([]byte) []byte) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return "ed25519:" + base64.StdEncoding.EncodeToString(pub), func(index []byte) []byte {
		b, _ := json.Marshal(map[string]string{"alg": "ed25519", "key_id": KeyID(pub), "sig": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, index))})
		return b
	}
}

func TestCheckSignature(t *testing.T) {
	a, signA := testKey(t)
	b, signB := testKey(t)
	index := []byte(`{"market_version": 1, "name": "x"}`)
	keyA, _ := ParseSigningKey(a)

	cases := []struct {
		name          string
		sig           []byte
		trusted, hint string
		state, trust  string
		refused       bool
		pending       string
	}{
		{"unsigned, nothing trusted", nil, "", "", SigUnsigned, "", false, ""},
		{"first signature: trust on first use", signA(index), "", a, SigSigned, a, false, ""},
		{"first signature by a key the index does not name", signA(index), "", b, SigInvalid, "", true, ""},
		{"first signature without a hint", signA(index), "", "", SigInvalid, "", true, ""},
		{"signed by the trusted key", signA(index), a, a, SigSigned, a, false, ""},
		{"the hint is never the trust: a wrong hint changes nothing", signA(index), a, b, SigSigned, a, false, ""},
		{"tampered", signA([]byte(`{"other": 1}`)), a, a, SigInvalid, a, true, ""},
		{"signature gone", nil, a, a, SigInvalid, a, true, ""},
		{"key changed, the index names the new one", signB(index), a, b, SigKeyChanged, a, true, b},
		{"key changed, the index names another", signB(index), a, a, SigKeyChanged, a, true, ""},
		{"garbage", []byte("not json"), a, a, SigInvalid, a, true, ""},
		{"not a signature, nothing trusted yet: invalid", []byte(`{"market_version": 1}`), "", a, SigInvalid, "", true, ""},
	}
	for _, c := range cases {
		s, trust, why := CheckSignature(index, c.sig, c.trusted, c.hint)
		if s.State != c.state || trust != c.trust || (why != "") != c.refused || s.Pending != c.pending || s.Bad() != c.refused {
			t.Errorf("%s: %+v trust=%q why=%q", c.name, s, trust, why)
		}
	}
	if s, _, _ := CheckSignature(index, nil, a, a); !s.Unsign {
		t.Error("a signature gone may be accepted by the owner")
	}
	if s, _, _ := CheckSignature(index, signA(index), a, a); s.KeyID != KeyID(keyA) || len(s.KeyID) != 16 {
		t.Errorf("key id %q", s.KeyID)
	}
}

func TestCheckSignatureMalformed(t *testing.T) {
	key, _ := testKey(t)
	index := []byte(`{"market_version": 1, "name": "x"}`)
	cases := []struct {
		name string
		sig  []byte
	}{
		{"empty file", []byte{}},
		{"invalid JSON", []byte("not json")},
		{"null", []byte("null")},
		{"wrong algorithm", []byte(`{"alg":"rsa","key_id":"example","sig":"AA=="}`)},
		{"missing key id", []byte(`{"alg":"ed25519","sig":"AA=="}`)},
		{"missing signature", []byte(`{"alg":"ed25519","key_id":"example"}`)},
		{"too large", []byte(strings.Repeat(" ", sigFileMost+1))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, trusted := range []string{"", key} {
				for _, hint := range []string{"", key} {
					s, trust, why := CheckSignature(index, c.sig, trusted, hint)
					if !s.Bad() || trust != trusted || why == "" || s.Pending != "" || s.Unsign {
						t.Errorf("trusted=%q hint=%q: %+v trust=%q why=%q", trusted, hint, s, trust, why)
					}
				}
			}
		})
	}
}
