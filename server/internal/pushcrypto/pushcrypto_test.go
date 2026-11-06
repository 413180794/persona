package pushcrypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	pub, err := PublicKey(base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(`{"aps":{"alert":{"title":"Claude 在问你","body":"要继续吗"}}}`)
	sealed, err := Seal(pub, msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("Claude")) || len(sealed) != 65+len(msg)+16 {
		t.Fatalf("sealed = %d bytes, want enc + ciphertext + tag", len(sealed))
	}
	got, err := Open(priv, sealed)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("open = %q, %v", got, err)
	}
}

func TestOpenRejectsOtherKeyAndTampering(t *testing.T) {
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	other, _ := ecdh.P256().GenerateKey(rand.Reader)
	sealed, err := Seal(priv.PublicKey(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(other, sealed); err == nil {
		t.Fatal("opened with another key")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Open(priv, sealed); err == nil {
		t.Fatal("opened a tampered payload")
	}
}

func TestPublicKeyRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := PublicKey(s); err == nil {
			t.Errorf("PublicKey(%q) accepted", s)
		}
	}
}

// vector is shared with apple/PersonaTests/PushCryptoTests.swift: CryptoKit must open what Go sealed.
var vector = struct{ priv, sealed, plain string }{
	"el2PlP5fa1q1OuNd1M8/leos7rSG4OzZEtfcyKlg6+c=",
	"BPiOTTGm5iJ8tQtSQqbHrovSzSW2yT+HV5ZEVWNfR5MxHcfJwmgfSpX9ObAWjgpGZDtRFUECe+QM3aLYLNx8tpxnkHQpRVbQB7BPH5sM3hC3TnI2ytruFlACROnZ5eckOeENMvMR95KJh7/d65/VCaK+zcafUX8sfqLHz8JCMfHOhPpFiBHMNej4OmTTkZ1ri6s81YWRb6PJ0D2gqe8PYnI9BE5vliK+hABGKhOQs+mUmXVhK2MbZerzwkFRCzYMJQEHd/NzaBHB78QCDQMy/hCRITH6EQyH8N2DX/79GZ1GKzqYC3f6oJYFZIZw/sKbbkmGlUnqOrN9gRCSERHyW+Lbc1fpeBqoDVV6F86z8xcTWygZ79I4ssqh2poGguuxeZWaMF6+hyE+xK3Ou9CVXLcGXiYSxCh9",
	`{"aps":{"alert":{"title":"Claude 在问你","subtitle":"重构 · 笔记本","body":"要继续吗？"},"category":"INTERACTION_QUESTION","thread-id":"s1"},"interaction_id":"ix1","machine_id":"m1","session_id":"s1","iat":1798000000}`,
}

func TestSharedVectorOpens(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(vector.priv)
	priv, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := base64.StdEncoding.DecodeString(vector.sealed)
	got, err := Open(priv, sealed)
	if err != nil || string(got) != vector.plain {
		t.Fatalf("open = %q, %v", got, err)
	}
}
