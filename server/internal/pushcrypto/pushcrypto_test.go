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

// vector is shared with apple/JarvisTests/PushCryptoTests.swift: CryptoKit must open what Go sealed.
var vector = struct{ priv, sealed, plain string }{
	"el2PlP5fa1q1OuNd1M8/leos7rSG4OzZEtfcyKlg6+c=",
	"BCggIsspkEoE9Iv+kSeqwGyPHxVNL0Cuto0IV/ZcubpAjXlNzTrF6T60wHSyaJiGj9b4udL2LnPDJEydaRAUnm/4syS2NCzzS/iffzfQTIx+WpKnYO5yqff6oDa/z7t7ow2jocpUOakeY4YKHS7ExN0Vwf5lnqWtOl282TY2JPHGUhI1ZKSiwB0RM8CzPIEzgHF0NWJldVg2pWRuHMGetwweyi8TUM2Zq0AzFPWvfbp8XWjshd1QjOQtoarB/ICzOKNLgXgpyJumYwxSvLw8m1+A6IZpF7Xl8UQwOK1Ytf/OoXJWgenoHY1XmFXBTr66RK0A3QgkM/1scEeQIvyL28MeDJzHdDO5u6n5Em3BuZLvaB8OSehyZHbPHPeMlabYso/z/lTsI4mPA0cP+6vNd+I5M6yUAPmi",
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
