package extwire

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestProof(t *testing.T) {
	key := ProofKey("pbus_token")
	if sum := sha256.Sum256([]byte("pbus_token")); key == hex.EncodeToString(sum[:]) || key == ProofKey("pbus_other") {
		t.Fatal("the key is the token's stored hash, or not the token's own")
	}
	now := time.Now()
	v := SignProof(key, "work", "GET", "/api/v1/machines?x=1", "pext_a", now)
	p, err := ParseProof(v)
	if err != nil || p.Manager != "work" || p.Time.Unix() != now.Unix() || !p.Fresh(now) {
		t.Fatalf("%q: %+v %v", v, p, err)
	}
	if !p.Valid(key, "GET", "/api/v1/machines?x=1", "pext_a") {
		t.Fatal("its own request")
	}
	for _, c := range []struct{ key, method, uri, tok string }{
		{ProofKey("pbus_other"), "GET", "/api/v1/machines?x=1", "pext_a"},
		{"", "GET", "/api/v1/machines?x=1", "pext_a"},
		{key, "POST", "/api/v1/machines?x=1", "pext_a"},
		{key, "GET", "/api/v1/machines", "pext_a"},
		{key, "GET", "/api/v1/machines?x=1", "pext_b"},
	} {
		if p.Valid(c.key, c.method, c.uri, c.tok) {
			t.Errorf("valid for %+v", c)
		}
	}
	if p.Fresh(now.Add(ProofWindow+time.Second)) || p.Fresh(now.Add(-ProofWindow-time.Second)) {
		t.Fatal("fresh outside the window")
	}
	if SignProof(key, "work", "GET", "/", "pext_a", now) == SignProof(key, "work", "GET", "/", "pext_a", now) {
		t.Fatal("two proofs with one nonce")
	}
	parts := strings.Split(v, ",")
	for _, bad := range []string{"", "v1", "v2" + v[2:], v + ",x=1", strings.Replace(v, "m=work", "m=Work", 1),
		strings.Replace(v, parts[2], "t=0", 1), strings.Replace(v, parts[2], "t=01", 1), strings.Replace(v, parts[3], "n=short", 1),
		strings.Replace(v, parts[4], "s=AAAA", 1), parts[0] + "," + parts[2] + "," + parts[1] + "," + parts[3] + "," + parts[4]} {
		if _, err := ParseProof(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}
