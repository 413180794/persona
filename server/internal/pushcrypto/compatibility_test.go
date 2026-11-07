package pushcrypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/rand"
	"testing"
)

func TestPreCutoverPushEncryptionContract(t *testing.T) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private, err := hpke.NewDHKEMPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := hpke.NewDHKEMPublicKey(key.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"aps":{"content-available":1},"jarvis_wake":"fixture"}`)
	sealed, err := Seal(key.PublicKey(), body)
	if err != nil {
		t.Fatal(err)
	}
	old, err := hpke.Open(private, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte("jarvis push v1"), sealed)
	if err != nil || !bytes.Equal(old, body) {
		t.Fatal("baseline app cannot open new hub push", err)
	}
	for _, info := range []string{"jarvis push v1", "persona push v1"} {
		sealed, err := hpke.Seal(public, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(info), body)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Open(key, sealed)
		if err != nil || !bytes.Equal(decoded, body) {
			t.Fatal(info, err)
		}
		sealed[len(sealed)-1] ^= 1
		if _, err := Open(key, sealed); err == nil {
			t.Fatal("tampered envelope accepted")
		}
	}
}
