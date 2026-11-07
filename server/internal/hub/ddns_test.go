package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/netaddr"
)

func ddnsTestAddress(last byte) netaddr.Addr {
	// Synthetic global addresses exercise classification without deployment fixtures.
	return netaddr.Addr{
		IP: netip.AddrFrom16([16]byte{0x20, 15: last}), Interface: "eth0",
		InterfaceFlags: net.FlagUp, FlagsKnown: true, ScopeKnown: true,
		Flags: netaddr.FlagPermanent,
	}
}

func TestDDNSCloudflareAddressChange(t *testing.T) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(secret[:])
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old, next := ddnsTestAddress(1), ddnsTestAddress(2)
	var lists, patches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing bearer authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			lists.Add(1)
			if r.URL.Path != "/client/v4/zones/zone/dns_records" || r.URL.Query().Get("type") != "AAAA" || r.URL.Query().Get("name") != "example.com" {
				t.Error("incorrect Cloudflare record lookup")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "record", "content": old.IP.String()}}})
		case http.MethodPatch:
			patches.Add(1)
			if r.URL.Path != "/client/v4/zones/zone/dns_records/record" {
				t.Error("incorrect Cloudflare PATCH path")
			}
			var body struct {
				Type, Name, Content string
				TTL                 int
				Proxied             bool
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Type != "AAAA" || body.Name != "example.com" || body.Content != next.IP.String() || body.TTL != 300 || body.Proxied {
				t.Error("incorrect Cloudflare PATCH payload")
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
		default:
			t.Error("unexpected Cloudflare method")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	d := newDDNSUpdater(DDNSConfig{Provider: "cloudflare", Record: "example.com", ZoneID: "zone", TokenFile: tokenFile, TTL: 300}, nil, nil)
	d.api = server.URL + "/client/v4"
	current := old
	d.list = func() ([]netaddr.Addr, error) {
		return []netaddr.Addr{current, {IP: netip.AddrFrom4([4]byte{0xc0, 0, 3, 1})}}, nil
	}
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if patches.Load() != 0 {
		t.Fatal("an already matching DNS address was patched")
	}
	current = next
	for i := 0; i < 2; i++ {
		if err := d.update(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if lists.Load() != 2 || patches.Load() != 1 {
		t.Fatalf("got %d lookups and %d patches, want 2 and 1", lists.Load(), patches.Load())
	}
	if err := os.Chmod(tokenFile, 0644); err != nil {
		t.Fatal(err)
	}
	current = ddnsTestAddress(3)
	if err := d.update(context.Background()); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatal("insecure token file was accepted")
	}
	if lists.Load() != 2 || patches.Load() != 1 {
		t.Fatal("insecure token file reached the provider")
	}
}

func TestDDNSWebhookRetryAndSelection(t *testing.T) {
	stable := ddnsTestAddress(5)
	unknown, temporary, deprecated := ddnsTestAddress(1), ddnsTestAddress(2), ddnsTestAddress(3)
	unknown.FlagsKnown = false
	temporary.Flags |= netaddr.FlagTemporary
	deprecated.Flags |= netaddr.FlagDeprecated
	private := netip.AddrFrom4([4]byte{10, 0, 0, 1})
	public := netip.AddrFrom4([4]byte{0xc0, 0, 3, 1})
	var calls atomic.Int32
	received := make(chan ddnsAddresses, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect webhook method or content type")
		}
		var body ddnsAddresses
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("response body must not be logged"))
		}
		received <- body
	}))
	defer server.Close()
	var logs bytes.Buffer
	d := newDDNSUpdater(DDNSConfig{Provider: "webhook", Record: "example.com", WebhookURL: server.URL}, []string{private.String(), "example.com", public.String()}, log.New(&logs, "", 0).Printf)
	d.list = func() ([]netaddr.Addr, error) {
		return []netaddr.Addr{unknown, temporary, deprecated, stable}, nil
	}
	d.retryMin, d.retryMax = time.Millisecond, time.Millisecond
	d.wake = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.run(ctx)
	}()
	defer func() { cancel(); <-done }()
	for i := 0; i < 2; i++ {
		select {
		case got := <-received:
			if got != (ddnsAddresses{Record: "example.com", IPv6: stable.IP.String(), IPv4: public.String()}) {
				t.Fatal("webhook published an unexpected address")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("webhook retry did not arrive")
		}
	}
	// An unbuffered wake joins the successful attempt before cancellation.
	select {
	case d.wake <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("DDNS worker did not finish reconciliation")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DDNS worker did not stop")
	}
	if err := d.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("unchanged webhook addresses were republished")
	}
	if !strings.Contains(logs.String(), "HTTP 503") || strings.Contains(logs.String(), "response body must not be logged") {
		t.Fatal("DDNS failure logging did not redact the response")
	}
}
