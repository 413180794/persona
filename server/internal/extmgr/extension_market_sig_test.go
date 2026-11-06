package extmgr

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

// marketKey is a market's signing key: its text and a signer of index bytes (a .sig file).
type marketKey struct {
	text string
	sign func([]byte) []byte
}

func newMarketKey(t *testing.T) marketKey {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return marketKey{"ed25519:" + base64.StdEncoding.EncodeToString(pub), func(index []byte) []byte {
		b, _ := json.Marshal(map[string]string{"alg": "ed25519", "key_id": extension.KeyID(pub), "sig": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, index))})
		return b
	}}
}

func keyIDText(t *testing.T, text string) string {
	k, err := extension.ParseSigningKey(text)
	if err != nil {
		t.Fatal(err)
	}
	return extension.KeyID(k)
}

// A signed market (docs/extensions.md "Signatures"): its key is trusted on first use; an index that was changed, lost its
// signature or is signed by another key is not taken (the last good one stays) and nothing installs from it; the owner
// trusts a new key, never a signature that does not verify.
func TestExtensionMarketSignature(t *testing.T) {
	h, phone, _ := marketHub(t)
	mux := h.routes()
	dir := uiPackage(t)
	pkg := tarDir(t, dir)
	gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: map[string][]byte{"example-ui-1.0.0.tar.gz": pkg}}
	srv := httptest.NewTLSServer(gh)
	defer srv.Close()
	h.extensions.markets.client, h.extensions.markets.githubAPI = srv.Client(), srv.URL

	a, b := newMarketKey(t), newMarketKey(t)
	entry := marketEntry(t, dir, "release:v1/example-ui-1.0.0.tar.gz", pkg)
	index := func(name, key string) []byte {
		ix, _ := json.Marshal(map[string]any{"market_version": 1, "name": name, "signing_key": key, "extensions": []any{entry}})
		return ix
	}
	publish := func(ix, sig []byte) {
		gh.mu.Lock()
		defer gh.mu.Unlock()
		gh.files[extension.IndexFile] = ix
		if sig == nil {
			delete(gh.files, extension.IndexFile+extension.SigSuffix)
		} else {
			gh.files[extension.IndexFile+extension.SigSuffix] = sig
		}
	}
	signature := func(m map[string]any) (string, string) {
		s, _ := m["signature"].(map[string]any)
		return fmt.Sprint(s["state"]), fmt.Sprint(s["key_id"])
	}
	base := "/api/v1/extensions/-/markets"
	installable := func(id string) (int, map[string]any) {
		return call(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market": %q, "extension": "example-ui"}`, id))
	}

	ix := index("Signed", a.text)
	publish(ix, a.sign(ix))
	code, mk := call(t, mux, "POST", base, phone, `{"spec": "github:example/plugins"}`)
	if st, kid := signature(mk); code != 200 || st != "signed" || kid != keyIDText(t, a.text) {
		t.Fatalf("added: %d %v", code, mk)
	}
	id := mk["id"].(string)
	if code, out := installable(id); code != 200 || out["preview_id"] == nil {
		t.Fatalf("a signed market installs: %d %v", code, out)
	}

	// Tampered: the new index is not taken, the market says why, nothing installs, not even from the last good index.
	publish(index("Tampered", a.text), a.sign(ix))
	code, out := call(t, mux, "POST", base+"/"+id+"/refresh", phone, "")
	if code != 400 || out["code"] != "market_signature" {
		t.Fatalf("tampered: %d %v", code, out)
	}
	_, list := call(t, mux, "GET", base, phone, "")
	m := list["markets"].([]any)[0].(map[string]any)
	if st, _ := signature(m); st != "invalid" || m["name"] != "Signed" || !strings.Contains(fmt.Sprint(m["error"]), "签名") {
		t.Fatalf("tampered market: %v", m)
	}
	if code, out := installable(id); code != 400 || !strings.Contains(fmt.Sprint(out["error"]), "签名") {
		t.Fatalf("installed from a tampered market: %d %v", code, out)
	}
	_, cat := call(t, mux, "GET", "/api/v1/extensions/-/catalog", phone, "")
	if it := cat["extensions"].([]any)[0].(map[string]any); !strings.Contains(fmt.Sprint(it["problem"]), "签名") {
		t.Fatalf("the catalog says why: %v", it)
	}
	if code, _ := call(t, mux, "POST", base+"/"+id+"/trust", phone, ""); code != 400 {
		t.Fatal("trusted a signature that does not verify")
	}

	// Back to a good signature: signed again.
	publish(ix, a.sign(ix))
	if code, out := call(t, mux, "POST", base+"/"+id+"/refresh", phone, ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}

	// The signature gone: refused until the owner accepts it unsigned.
	publish(index("Unsigned now", a.text), nil)
	if code, out := call(t, mux, "POST", base+"/"+id+"/refresh", phone, ""); code != 400 || out["code"] != "market_signature" {
		t.Fatalf("signature gone: %d %v", code, out)
	}

	// Another key, named by the index: key_changed until trusted; then signed by the new key, and installable.
	ixB := index("New key", b.text)
	publish(ixB, b.sign(ixB))
	call(t, mux, "POST", base+"/"+id+"/refresh", phone, "")
	_, list = call(t, mux, "GET", base, phone, "")
	m = list["markets"].([]any)[0].(map[string]any)
	if st, kid := signature(m); st != "key_changed" || kid != keyIDText(t, b.text) || m["name"] != "Signed" {
		t.Fatalf("key changed: %v", m)
	}
	if code, _ := installable(id); code != 400 {
		t.Fatal("installed from a market whose key changed")
	}
	// A move to another address does not get round the key either.
	if code, _ := call(t, mux, "PUT", base+"/"+id, phone, `{"spec": "github:example/plugins@main"}`); code != 400 {
		t.Fatal("a move took a new key")
	}
	code, mk = call(t, mux, "POST", base+"/"+id+"/trust", phone, "")
	if st, kid := signature(mk); code != 200 || st != "signed" || kid != keyIDText(t, b.text) || mk["name"] != "New key" {
		t.Fatalf("trusted the new key: %d %v", code, mk)
	}
	if code, _ := installable(id); code != 200 {
		t.Fatal("not installable after trust")
	}
	// From now on the old key is the stranger.
	publish(ix, a.sign(ix))
	if code, _ := call(t, mux, "POST", base+"/"+id+"/refresh", phone, ""); code != 400 {
		t.Fatal("the old key was taken back")
	}
	// The owner accepts that the market stopped signing: unsigned, read as before.
	publish(index("Unsigned", ""), nil)
	call(t, mux, "POST", base+"/"+id+"/refresh", phone, "")
	if code, mk = call(t, mux, "POST", base+"/"+id+"/trust", phone, ""); code != 200 {
		t.Fatalf("%d %v", code, mk)
	}
	if st, _ := signature(mk); st != "unsigned" || mk["name"] != "Unsigned" {
		t.Fatalf("accepted unsigned: %v", mk)
	}
	// The old paths show the signature too.
	if _, out := call(t, mux, "GET", "/api/v1/plugins/-/markets", phone, ""); out["markets"].([]any)[0].(map[string]any)["signature"] == nil {
		t.Fatalf("old path: %v", out)
	}
}

// A market that never signed is read as before, unsigned; a signed one whose signing_key is not the key that signed
// it cannot be added.
func TestExtensionMarketUnsigned(t *testing.T) {
	h, phone, _ := marketHub(t)
	mux := h.routes()
	a, b := newMarketKey(t), newMarketKey(t)
	files := map[string][]byte{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := files[strings.TrimPrefix(r.URL.Path, "/m/")]; ok {
			_, _ = w.Write(f)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	h.extensions.markets.client = srv.Client()
	files[extension.IndexFile] = []byte(`{"market_version": 1, "name": "Plain", "extensions": []}`)
	code, mk := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, fmt.Sprintf(`{"spec": %q}`, srv.URL+"/m/"))
	if s, _ := mk["signature"].(map[string]any); code != 200 || s["state"] != "unsigned" {
		t.Fatalf("%d %v", code, mk)
	}
	ix := []byte(fmt.Sprintf(`{"market_version": 1, "name": "Lying", "signing_key": %q, "extensions": []}`, b.text))
	files["other/"+extension.IndexFile], files["other/"+extension.IndexFile+extension.SigSuffix] = ix, a.sign(ix)
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, fmt.Sprintf(`{"spec": %q}`, srv.URL+"/m/other/")); code != 400 {
		t.Fatalf("added a market whose signature is not its key's: %d %v", code, out)
	}

	for _, sig := range [][]byte{[]byte("not json"), []byte(`{"market_version": 1}`), {}} {
		files["broken/"+extension.IndexFile] = files[extension.IndexFile]
		files["broken/"+extension.IndexFile+extension.SigSuffix] = sig
		if code, out := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, fmt.Sprintf(`{"spec": %q}`, srv.URL+"/m/broken/")); code != 400 || !strings.Contains(fmt.Sprint(out["error"]), "签名") {
			t.Fatalf("added a market with a malformed first signature: %d %v", code, out)
		}
	}
	if _, out := call(t, mux, "GET", "/api/v1/extensions/-/markets", phone, ""); len(out["markets"].([]any)) != 1 {
		t.Fatalf("refused markets were persisted: %v", out)
	}
}
