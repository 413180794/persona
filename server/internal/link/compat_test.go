package link

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCompatPairSchemesKeepSecurityRequirements(t *testing.T) {
	q := url.Values{"url": {"http://hub.example.com:80"}, "tls": {"443"}, "token": {"jp_example"}, "hub": {"hub"}, "fp": {"pin"}, "machine": {"laptop"}}
	for _, host := range []string{"pair", "link"} {
		canonical := "persona://" + host + "?" + q.Encode()
		wantToken, want, err := parsePairLink(canonical)
		if err != nil {
			t.Fatal(err)
		}
		token, got, err := parsePairLink(" " + LegacyPairLink(canonical) + "\n")
		if err != nil || token != wantToken || !reflect.DeepEqual(got, want) {
			t.Fatalf("%+v %v", got, err)
		}
		for _, key := range []string{"token", "tls", "url"} {
			bad := maps.Clone(q)
			bad.Del(key)
			if _, _, err := parsePairLink("jarvis://" + host + "?" + bad.Encode()); err == nil {
				t.Fatalf("legacy link accepted without %s", key)
			}
		}
	}
	for _, raw := range []string{"jarvis://open?" + q.Encode(), "other://pair?" + q.Encode(), "jarvis://pair?%zz"} {
		if _, _, err := parsePairLink(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	proxy := "jarvis://link?url=" + url.QueryEscape("https://hub.example.com/prefix") + "&token=x&machine=laptop"
	if _, c, err := parsePairLink(proxy); err != nil || !c.SystemTrust() {
		t.Fatalf("system trust: %+v %v", c, err)
	}
	if _, _, err := parsePairLink(strings.Replace(proxy, "https", "http", 1)); err == nil {
		t.Fatal("legacy proxy allowed HTTP")
	}
}

func TestCompatConnectorSchemesOneInvitation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	inv, err := PairListen(path, "", []string{"hub.example.com:18663"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if inv.LegacyLink != LegacyPairLink(inv.Link) {
		t.Fatal("legacy output missing")
	}
	want, fp, code, err := ParseConnectorLink(inv.Link)
	if err != nil {
		t.Fatal(err)
	}
	got, oldFP, oldCode, err := ParseConnectorLink(inv.LegacyLink)
	if err != nil || !reflect.DeepEqual(got, want) || oldFP != fp || oldCode != code {
		t.Fatalf("%v %s %s %v", got, oldFP, oldCode, err)
	}
	cfg, err := Read(path)
	if err != nil || cfg.Passive.Code.SHA256 != hashHex(code) {
		t.Fatal("URLs do not share saved code")
	}
	for _, key := range []string{"addr", "fp", "code"} {
		u, _ := url.Parse(inv.LegacyLink)
		q := u.Query()
		q.Del(key)
		u.RawQuery = q.Encode()
		if _, _, _, err := ParseConnectorLink(u.String()); err == nil {
			t.Fatalf("legacy connector accepted without %s", key)
		}
	}
}

func TestCompatTLSIdentityKeepsLegacyCertificateAndPin(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	id, err := loadIdentity(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	originalPin := id.fp
	keyBytes, _ := os.ReadFile(filepath.Join(dir, "link.key"))
	leaf := *id.crt.Leaf
	leaf.DNSNames = []string{"connector.jarvis.invalid"}
	der, err := x509.CreateCertificate(rand.Reader, &leaf, &leaf, &id.key.PublicKey, id.key)
	if err != nil {
		t.Fatal(err)
	}
	certBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "link.crt"), certBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadIdentity(dir, now)
	if err != nil || loaded.fp != originalPin || loaded.crt.Leaf.DNSNames[0] != "connector.jarvis.invalid" {
		t.Fatalf("legacy identity changed: %v", err)
	}
	gotKey, _ := os.ReadFile(filepath.Join(dir, "link.key"))
	gotCert, _ := os.ReadFile(filepath.Join(dir, "link.crt"))
	if !bytes.Equal(gotKey, keyBytes) || !bytes.Equal(gotCert, certBytes) {
		t.Fatal("existing TLS files replaced")
	}
	if err := loaded.renew(now.Add(certLife - certRenew + time.Hour)); err != nil {
		t.Fatal(err)
	}
	if loaded.fp != originalPin || loaded.crt.Leaf.DNSNames[0] != tlsName {
		t.Fatal("renewal changed the pin")
	}
}

// The exported pair: what the Windows tray asks Windows to open and accepts as a pairing link.
func TestPairSchemesAreTheCurrentAndTheBaseline(t *testing.T) {
	if got := PairSchemes(); len(got) != 2 || got[0] != "persona" || got[1] != legacyPairScheme {
		t.Fatalf("%q", got)
	}
	for _, s := range []string{"persona", "PERSONA", legacyPairScheme, strings.ToUpper(legacyPairScheme)} {
		if !PairScheme(s) {
			t.Errorf("%q not a pairing scheme", s)
		}
	}
	for _, s := range []string{"", "https", "persona-link", "j"} {
		if PairScheme(s) {
			t.Errorf("%q taken for a pairing scheme", s)
		}
	}
}
