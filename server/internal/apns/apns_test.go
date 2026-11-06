package apns

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKey writes a fresh P-256 key as a PKCS#8 PEM, the shape of Apple's .p8 file.
func writeKey(t *testing.T) string {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNewRequiresKeyIDAndTeamID: a config without key_id or team_id would sign tokens Apple refuses with 403
// InvalidProviderToken, so New refuses it at boot.
func TestNewRequiresKeyIDAndTeamID(t *testing.T) {
	key := writeKey(t)
	for _, tc := range []struct {
		name, keyID, teamID, want string
	}{
		{"empty key_id", "", "TEAM123456", "key_id is empty"},
		{"empty team_id", "KEY1234567", "", "team_id is empty"},
		{"blank key_id", "  \t", "TEAM123456", "key_id is empty"},
		{"blank team_id", "KEY1234567", " \n", "team_id is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{KeyID: tc.keyID, TeamID: tc.teamID, KeyPath: key})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestNewTrimsIDs: surrounding whitespace in the ids is dropped, so the token carries the bare ids.
func TestNewTrimsIDs(t *testing.T) {
	c, err := New(Config{KeyID: " KEY1234567 ", TeamID: "TEAM123456\n", KeyPath: writeKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	if c.Cfg.KeyID != "KEY1234567" || c.Cfg.TeamID != "TEAM123456" {
		t.Fatalf("Cfg = %+v, want trimmed ids", c.Cfg)
	}
	if _, err := c.token(); err != nil {
		t.Fatal(err)
	}
}
