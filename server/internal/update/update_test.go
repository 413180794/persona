package update

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
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

func TestBuildManifest(t *testing.T) {
	dir := t.TempDir()
	bin := []byte("a personad for linux")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(bin)
	zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "personad-linux-amd64.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(dir, Manifest{Version: "v1", Build: 202611021530, Protocol: 1})
	if err != nil {
		t.Fatal(err)
	}
	f := m.Files["linux-amd64"]
	outer, inner := sha256.Sum256(gz.Bytes()), sha256.Sum256(bin)
	if f.Name != "personad-linux-amd64.gz" || f.Size != int64(gz.Len()) || f.SHA256 != hex.EncodeToString(outer[:]) || f.BinarySHA256 != hex.EncodeToString(inner[:]) {
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
