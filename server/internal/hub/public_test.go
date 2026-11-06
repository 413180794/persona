package hub

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

type publicSettingsStore struct {
	sharedStore
	id  string
	err error
}

func (s *publicSettingsStore) Setting(_ context.Context, key string, out any) (bool, error) {
	if key != "hub_id" {
		return false, fmt.Errorf("unexpected setting %q", key)
	}
	*(out.(*string)) = s.id
	return s.id != "", s.err
}

func (s *publicSettingsStore) PutSetting(_ context.Context, key string, value any) error {
	if key != "hub_id" {
		return fmt.Errorf("unexpected setting %q", key)
	}
	if s.err == nil {
		s.id = value.(string)
	}
	return s.err
}

func TestPublicHubID(t *testing.T) {
	store := &publicSettingsStore{}
	h := &Hub{store: store}
	if err := h.initHubID(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := h.hubID()
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("invalid UUIDv4: %q", id)
	}
	again := &Hub{store: store}
	if err := again.initHubID(context.Background()); err != nil || again.hubID() != id {
		t.Fatalf("hub id was not persisted: %v", err)
	}
	memory := &Hub{}
	if memory.hubID() == "" || memory.hubID() != memory.hubID() || memory.hubID() == id {
		t.Fatal("database-free hub id is not stable or unique")
	}
	broken := &Hub{store: &publicSettingsStore{err: errors.New("unavailable")}}
	if err := broken.initHubID(context.Background()); err == nil || broken.hubID() != "" {
		t.Fatal("database failure silently changed the identity")
	}
}

func TestPublicHubIDDatabase(t *testing.T) {
	dsn := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	database, err := db.Open(ctx, dsn, db.HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	first := &Hub{store: database}
	if err := first.initHubID(ctx); err != nil {
		t.Fatal(err)
	}
	var stored string
	if ok, err := database.Setting(ctx, "hub_id", &stored); !ok || err != nil || stored != first.hubID() {
		t.Fatalf("hub id is not in settings: %v", err)
	}
	second := &Hub{store: database}
	if err := second.initHubID(ctx); err != nil || second.hubID() != first.hubID() {
		t.Fatalf("hub id changed on restart: %v", err)
	}
}

func newPublicTestHub(t *testing.T) *Hub {
	t.Helper()
	return &Hub{
		cfg:    &Config{Listen: "127.0.0.1:0", Token: "test-token", PublicURL: "http://example.com:18660", dir: t.TempDir()},
		logger: log.New(io.Discard, "", 0), version: "test-version",
		clients: map[*client]struct{}{},
	}
}

func runPublicTestHub(t *testing.T, h *Hub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := h.startPublic(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.serveHTTP(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve HTTP: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("HTTP servers did not shut down")
		}
	})
}

func pinnedTestClient(pin string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		// Identity is checked against the paired SPKI, including for IP literals without SNI.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			sum := sha256.Sum256(cs.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if base64.RawURLEncoding.EncodeToString(sum[:]) != pin {
				return errors.New("pin mismatch")
			}
			return nil
		},
	}}}
}

func TestPublicTLSListenerAndRoutes(t *testing.T) {
	h := newPublicTestHub(t)
	h.cfg.Public.TLSListen = "127.0.0.1:0"
	runPublicTestHub(t, h)
	port, pin := h.tlsPin()
	if port == 0 || len(pin) != 43 {
		t.Fatal("TLS metadata is not ready before serving")
	}
	base := "https://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	client := pinnedTestClient(pin)
	defer client.CloseIdleConnections()
	resp, err := client.Get(base + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	err = json.NewDecoder(resp.Body).Decode(&health)
	resp.Body.Close()
	if err != nil || health["ok"] != true || resp.TLS.Version < tls.VersionTLS12 {
		t.Fatalf("TLS health: %v, %v", health, err)
	}
	for _, token := range []string{"", "test-token"} {
		req, _ := http.NewRequest("GET", base+"/api/v1/machines", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := http.StatusOK
		if token == "" {
			want = http.StatusUnauthorized
		}
		if resp.StatusCode != want {
			t.Fatalf("TLS route status = %d, want %d", resp.StatusCode, want)
		}
	}
	wrong := pinnedTestClient("wrong-pin")
	defer wrong.CloseIdleConnections()
	if resp, err := wrong.Get(base + "/api/v1/health"); err == nil {
		resp.Body.Close()
		t.Fatal("wrong pin was accepted")
	}
	for _, server := range []*http.Server{newHubHTTPServer(":0", h.routes()), h.public.server} {
		if server.ReadHeaderTimeout == 0 || server.IdleTimeout == 0 || server.MaxHeaderBytes > 64<<10 || server.MaxHeaderBytes == 0 {
			t.Fatal("missing HTTP header/idle limits")
		}
		if server.ReadTimeout != 0 || server.WriteTimeout != 0 {
			t.Fatal("whole-request timeout can interrupt WebSockets or uploads")
		}
	}
}

func TestPublicTLSConfigurationErrors(t *testing.T) {
	for _, configure := range []func(*Hub){
		func(h *Hub) { h.cfg.Public.CertFile = "missing.crt" },
		func(h *Hub) { h.cfg.Public.KeyFile = "missing.key" },
		func(h *Hub) { h.cfg.Public.TLSListen = "invalid" },
	} {
		h := newPublicTestHub(t)
		h.cfg.Public.TLSListen = "127.0.0.1:0"
		configure(h)
		if err := h.startPublic(context.Background()); err == nil {
			t.Fatal("invalid TLS configuration was accepted")
		}
	}
}

func TestPublicTLSLiveReissueAndExternalFiles(t *testing.T) {
	h := newPublicTestHub(t)
	h.cfg.Public.TLSListen = "127.0.0.1:0"
	runPublicTestHub(t, h)
	port, pin := h.tlsPin()
	h.public.mu.RLock()
	serial := h.public.certificate.Leaf.SerialNumber.String()
	h.public.mu.RUnlock()
	// Close to expiry the certificate is signed again with the same key (the names never change: tlsNames).
	if err := h.refreshTLS(time.Now().Add(340 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	client := pinnedTestClient(pin)
	defer client.CloseIdleConnections()
	resp, err := client.Get("https://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(port)) + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	leaf := resp.TLS.PeerCertificates[0]
	if leaf.SerialNumber.String() == serial {
		t.Fatal("listener did not start serving the reissued certificate")
	}
	if _, after := h.tlsPin(); after != pin {
		t.Fatal("live reissue changed the pin")
	}

	source := t.TempDir()
	_, customPin, err := loadOrCreateTLS(source, []string{"example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	custom := newPublicTestHub(t)
	custom.cfg.Public = PublicConfig{TLSListen: "127.0.0.1:0", CertFile: filepath.Join(source, "hub.crt"), KeyFile: filepath.Join(source, "hub.key")}
	runPublicTestHub(t, custom)
	customPort, actualPin := custom.tlsPin()
	if actualPin != customPin {
		t.Fatal("configured certificate was not used")
	}
	customClient := pinnedTestClient(customPin)
	defer customClient.CloseIdleConnections()
	resp, err = customClient.Get("https://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(customPort)) + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := os.Stat(filepath.Join(custom.cfg.dir, "tls")); !os.IsNotExist(err) {
		t.Fatal("custom certificates created an unrelated managed identity")
	}
	custom.cfg.Public.CertFile, custom.cfg.Public.KeyFile = filepath.Join(h.cfg.dir, "tls", "hub.crt"), filepath.Join(h.cfg.dir, "tls", "hub.key")
	if err := custom.refreshTLS(time.Now()); err == nil {
		t.Fatal("live identity rotation was accepted")
	}
	if _, after := custom.tlsPin(); after != customPin {
		t.Fatal("failed identity rotation discarded the running identity")
	}
}

func TestPublicListenerFailureClosesBothTransports(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	h := newPublicTestHub(t)
	h.cfg.Listen = occupied.Addr().String()
	h.cfg.Public.TLSListen = "127.0.0.1:0"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.startPublic(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.serveHTTP(ctx); err == nil {
		t.Fatal("plaintext listen failure was not propagated")
	}
	if conn, err := net.DialTimeout("tcp", h.public.listener.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("TLS listener was left open after plaintext startup failed")
	}
}

func TestCertificateCarriesNoAddresses(t *testing.T) {
	h := newPublicTestHub(t)
	h.cfg.Public.TLSListen = "127.0.0.1:0"
	h.cfg.Public.Advertise = []string{"https://hub.example.com:19443"}
	runPublicTestHub(t, h)
	h.public.mu.RLock()
	leaf := h.public.certificate.Leaf
	h.public.mu.RUnlock()
	if len(leaf.IPAddresses) != 0 || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != h.hubID()+".persona.invalid" {
		t.Fatalf("certificate names %v %v: anyone reaching the port can read them", leaf.DNSNames, leaf.IPAddresses)
	}
}

func TestHandshakeNoiseIsSummed(t *testing.T) {
	var lines []string
	now := time.Now()
	n := &handshakeNoise{logf: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }, now: func() time.Time { return now }}
	for i := 0; i < 50; i++ {
		fmt.Fprintf(n, "http: TLS handshake error from 192.0.2.1:%d: EOF\n", 1000+i)
	}
	if len(lines) != 0 {
		t.Fatalf("handshake errors logged one by one: %v", lines)
	}
	now = now.Add(handshakeSummaryEvery)
	fmt.Fprintf(n, "http: TLS handshake error from 192.0.2.1:2000: EOF\n")
	fmt.Fprintf(n, "http: panic serving 192.0.2.1:2001: boom\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "51 failed handshakes") || !strings.Contains(lines[1], "panic serving") {
		t.Fatalf("lines = %v", lines)
	}
}
