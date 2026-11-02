package hub

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestTLSIdentityRenewal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	now := time.Now().UTC().Truncate(time.Second)
	names := []string{"example.com", "192.0.2.1", "2001:db8::1"}
	cert, pin, err := loadOrCreateTLS(dir, names, now)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(pin) {
		t.Fatalf("invalid pin format: %q", pin)
	}
	if b, err := base64.RawURLEncoding.DecodeString(pin); err != nil || len(b) != 32 {
		t.Fatalf("invalid SHA-256 pin: %v", err)
	}
	key := cert.PrivateKey.(*ecdsa.PrivateKey)
	if key.Curve != elliptic.P256() {
		t.Fatal("identity is not P-256")
	}
	if err := cert.Leaf.CheckSignature(cert.Leaf.SignatureAlgorithm, cert.Leaf.RawTBSCertificate, cert.Leaf.Signature); err != nil {
		t.Fatalf("certificate is not self signed: %v", err)
	}
	for _, host := range names {
		if err := cert.Leaf.VerifyHostname(host); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct {
		path string
		mode os.FileMode
	}{{dir, 0o700}, {filepath.Join(dir, "hub.key"), 0o600}, {filepath.Join(dir, "hub.crt"), 0o600}} {
		st, err := os.Stat(entry.path)
		if err != nil || st.Mode().Perm() != entry.mode {
			t.Fatalf("TLS permissions: %v, %v", st, err)
		}
	}
	keyBefore, _ := os.ReadFile(filepath.Join(dir, "hub.key"))
	again, pinAgain, err := loadOrCreateTLS(dir, names, now.Add(24*time.Hour))
	if err != nil || pinAgain != pin || !bytes.Equal(cert.Certificate[0], again.Certificate[0]) {
		t.Fatalf("valid certificate was not reused: %v", err)
	}
	renewed, renewedPin, err := loadOrCreateTLS(dir, names, cert.Leaf.NotAfter.Add(-29*24*time.Hour))
	if err != nil || renewedPin != pin || bytes.Equal(cert.Certificate[0], renewed.Certificate[0]) {
		t.Fatalf("renewal changed identity or did not renew: %v", err)
	}
	keyAfter, _ := os.ReadFile(filepath.Join(dir, "hub.key"))
	if !bytes.Equal(keyBefore, keyAfter) || !renewed.Leaf.NotAfter.After(cert.Leaf.NotAfter) {
		t.Fatal("renewal replaced the key or did not extend validity")
	}
	changed, changedPin, err := loadOrCreateTLS(dir, append(names, "hub.example.com"), now)
	if err != nil || changedPin != pin || changed.Leaf.VerifyHostname("hub.example.com") != nil {
		t.Fatalf("SAN change did not preserve the identity: %v", err)
	}
}

func TestTLSIdentityRejectsBrokenKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.key")
	broken := []byte("invalid identity")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadOrCreateTLS(dir, []string{"example.com"}, time.Now()); err == nil {
		t.Fatal("broken key was accepted")
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, broken) {
		t.Fatal("broken key was silently replaced")
	}
}

func TestTLSConcurrentIdentityCreation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	const creators = 8
	pins := make(chan string, creators)
	errs := make(chan error, creators)
	var wg sync.WaitGroup
	for range creators {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, pin, err := loadOrCreateTLS(dir, []string{"example.com"}, time.Now())
			pins <- pin
			errs <- err
		}()
	}
	wg.Wait()
	close(pins)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := <-pins
	for pin := range pins {
		if pin != want {
			t.Fatal("concurrent creators used different keys")
		}
	}
}
