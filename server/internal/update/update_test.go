package update

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMake(t *testing.T) {
	for _, c := range []struct {
		channel, number, want string
	}{
		{"release", "202611021530", Release},
		{"release", "", Dev},           // no build number: it could not compare, it must not update
		{"release", "2026110215", Dev}, // not YYYYMMDDHHMM
		{"release", "202613021530", Dev},
		{"docker", "", Docker},
		{"public", "202611021530", Dev}, // the channel that was: nothing updates itself by accident
		{"", "202611021530", Dev},
	} {
		if got := Make("v", c.channel, c.number, "https://example.com/dl").Channel; got != c.want {
			t.Errorf("Make(%q, %q) = %s, want %s", c.channel, c.number, got, c.want)
		}
	}
	if Make("v", "release", "202611021530", "").SelfUpdates() {
		t.Error("a release with nowhere to look updates itself")
	}
	if !Make("v", "release", "202611021530", "https://example.com/dl").SelfUpdates() {
		t.Error("a release does not update itself")
	}
}

// A build deployed straight from master carries its build time: an older or equal release seen later never replaces
// it; a newer one does.
func TestNoDowngrade(t *testing.T) {
	deployed := Make("abc1234-202611021600", "release", "202611021600", "https://example.com/dl")
	if deployed.Newer(202611021500) {
		t.Error("an older release would downgrade a build deployed from master")
	}
	if deployed.Newer(202611021600) {
		t.Error("the same build number counts as newer")
	}
	if !deployed.Newer(202611021700) {
		t.Error("a newer release does not take over")
	}
	if (Build{}).Newer(202611021700) {
		t.Error("a build without a number updates")
	}
}

func testKey(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "k")
	pub, err := Keygen(path)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := LoadPrivate(path)
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func TestSignAndVerify(t *testing.T) {
	priv, pub := testKey(t)
	_, other := testKey(t)
	data := []byte(`{"version":"v1","build":202611021530}`)
	sig, _ := json.Marshal(Sign(priv, data))
	if err := Verify(data, sig, []ed25519.PublicKey{other, pub}); err != nil {
		t.Fatalf("a good signature: %v", err)
	}
	tampered := bytes.Replace(data, []byte("1530"), []byte("1531"), 1)
	if err := Verify(tampered, sig, []ed25519.PublicKey{pub}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("one byte changed: %v", err)
	}
	if err := Verify(data, sig, []ed25519.PublicKey{other}); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("a key not trusted: %v", err)
	}
	if err := Verify(data, nil, []ed25519.PublicKey{pub}); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("no signature: %v", err)
	}
	if err := Verify(data, sig, nil); !errors.Is(err, ErrNoTrustedKey) {
		t.Fatalf("no keys compiled in: %v", err)
	}
	if err := Verify(data, []byte(`{"key_id":"x","signature":"%%%"}`), []ed25519.PublicKey{pub}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("garbage: %v", err)
	}
}

func TestKeyFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k")
	if _, err := Keygen(path); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", st.Mode().Perm())
	}
	if _, err := Keygen(path); err == nil {
		t.Fatal("an existing key was overwritten")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrivate(path); err == nil {
		t.Fatal("a key others can read was used")
	}
}

func TestTrustedKeysSkipsMalformed(t *testing.T) {
	_, pub := testKey(t)
	saved := trustedKeys
	defer func() { trustedKeys = saved }()
	trustedKeys = map[string]string{
		KeyID(pub):         base64.StdEncoding.EncodeToString(pub),
		"0000000000000000": base64.StdEncoding.EncodeToString(pub), // id does not match the key
		"bad":              "%%%",
	}
	if got := TrustedKeys(); len(got) != 1 || !got[0].Equal(pub) {
		t.Fatalf("%d keys", len(got))
	}
}

func TestBuildManifest(t *testing.T) {
	dir := t.TempDir()
	bin := []byte("a jarvisd for linux")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(bin)
	zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "jarvisd-linux-amd64.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(dir, Manifest{Version: "v1", Build: 202611021530, Protocol: 1})
	if err != nil {
		t.Fatal(err)
	}
	f := m.Files["linux-amd64"]
	outer, inner := sha256.Sum256(gz.Bytes()), sha256.Sum256(bin)
	if f.Name != "jarvisd-linux-amd64.gz" || f.Size != int64(gz.Len()) || f.SHA256 != hex.EncodeToString(outer[:]) || f.BinarySHA256 != hex.EncodeToString(inner[:]) {
		t.Fatalf("%+v", f)
	}
	b, _ := json.Marshal(m)
	if back, err := ParseManifest(b); err != nil || back.Build != 202611021530 {
		t.Fatalf("round trip: %v", err)
	}
}

func TestCheckNext(t *testing.T) {
	served := &Manifest{Build: 202611021530, Commit: "aaa"}
	yes := func(string, string) (bool, error) { return true, nil }
	no := func(string, string) (bool, error) { return false, nil }
	broken := func(string, string) (bool, error) { return false, errors.New("unknown commit") }
	if err := CheckNext(nil, 202611021600, "bbb", no); err != nil {
		t.Errorf("first release: %v", err)
	}
	if err := CheckNext(served, 202611021530, "bbb", yes); err == nil {
		t.Error("the same build number was published again")
	}
	if err := CheckNext(served, 202611021400, "bbb", yes); err == nil {
		t.Error("an older build number was published")
	}
	if err := CheckNext(served, 202611021600, "bbb", no); err == nil {
		t.Error("a commit that does not follow the served one was published")
	}
	if err := CheckNext(served, 202611021600, "bbb", broken); err == nil {
		t.Error("an unknown served commit was let through")
	}
	if err := CheckNext(served, 202611021600, "bbb", yes); err != nil {
		t.Errorf("a proper next release: %v", err)
	}
}
