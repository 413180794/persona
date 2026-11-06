package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/update"
)

func TestHubShutdownWithOnlineDevice(t *testing.T) {
	url := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	database, err := db.Open(context.Background(), url, db.HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// Keep the real startup path isolated from local integrations and containers.
	fakeLark := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fakeLark.Close()
	for _, reading := range []bool{true, false} {
		name := "responsive"
		if !reading {
			name = "no-close-reply"
		}
		t.Run(name, func(t *testing.T) {
			var samples []time.Duration
			for range 5 {
				device, token := "shutdown-"+randomHex(8), "jd_"+randomHex(24)
				if err := database.CreateDeviceToken(context.Background(), db.DeviceToken{
					ID: device, Name: "test device", Kind: "phone", Role: roleFull,
				}, hashToken(token)); err != nil {
					t.Fatal(err)
				}
				home := t.TempDir()
				dir := filepath.Join(home, ".persona")
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PERSONA_HOME", "")
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
				loopback := netip.AddrFrom4([4]byte{127, 0, 0, 1}).String()
				ln, err := net.Listen("tcp", net.JoinHostPort(loopback, "0"))
				if err != nil {
					t.Fatal(err)
				}
				addr := ln.Addr().String()
				_ = ln.Close()
				cfg := Config{Listen: addr, PublicURL: "http://" + addr, Token: randomHex(24),
					DatabaseURL: url, Machines: []MachineConfig{},
					ShutdownTimeoutSeconds: 4}
				raw, _ := json.Marshal(cfg)
				if err := os.WriteFile(filepath.Join(dir, "hub.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				done := make(chan error, 1)
				go func() { done <- Run(ctx, update.Build{Version: "shutdown-test"}) }()
				clientCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
				var conn *websocket.Conn
				for clientCtx.Err() == nil {
					conn, _, err = websocket.Dial(clientCtx, "ws://"+addr+"/api/v1/ws", &websocket.DialOptions{
						HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
					})
					if err == nil {
						break
					}
					select {
					case err := <-done:
						stop()
						t.Fatalf("hub exited before connection: %v", err)
					default:
					}
					time.Sleep(10 * time.Millisecond)
				}
				if err != nil {
					stop()
					t.Fatal(err)
				}
				// Hello proves session registration completed before cancellation.
				if _, _, err := conn.Read(clientCtx); err != nil {
					stop()
					t.Fatal(err)
				}
				readDone := make(chan error, 1)
				if reading {
					go func() {
						for {
							if _, _, err := conn.Read(clientCtx); err != nil {
								readDone <- err
								return
							}
						}
					}()
				}
				start := time.Now()
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("Run: %v", err)
					}
				case <-time.After(4 * time.Second):
					t.Fatal("Run exceeded shutdown budget")
				}
				samples = append(samples, time.Since(start))
				if reading {
					select {
					case err := <-readDone:
						if websocket.CloseStatus(err) != websocket.StatusGoingAway {
							t.Fatalf("close status: %v", err)
						}
					case <-time.After(time.Second):
						t.Fatal("client reader did not stop")
					}
				}
				_ = conn.CloseNow()
				stop()
				history, err := database.DeviceSessions(context.Background(), device, start.Add(-time.Minute), time.Now())
				if err != nil || len(history.Sessions) != 1 {
					t.Fatalf("session history: %+v, %v", history, err)
				}
				session := history.Sessions[0]
				if session.DisconnectedAt == nil || session.Reason != "hub_stopped" || history.Summary.Disconnects != 0 {
					t.Fatalf("session not stopped cleanly: %+v", history)
				}
				var stacks bytes.Buffer
				_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
				for _, method := range []string{"(*Hub).serveWS", "(*client).writeWS", "(*Hub).stopWebSockets"} {
					if strings.Contains(stacks.String(), method) {
						t.Fatalf("websocket goroutine remained after Run: %s", method)
					}
				}
			}
			slices.Sort(samples)
			var total time.Duration
			for _, sample := range samples {
				total += sample
			}
			t.Logf("shutdown samples=%d (not a baseline): min=%s p50=%s p90=%s max=%s mean=%s",
				len(samples), samples[0], samples[2], samples[4], samples[4], total/time.Duration(len(samples)))
		})
	}
}

func TestShutdownTotalBudget(t *testing.T) {
	budget := newShutdownBudget(&Config{})
	budget.timeout = 40 * time.Millisecond
	defer budget.close()
	ctx, cancel := context.WithCancel(context.Background())
	blocked := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		<-budget.started
		cancel()
	}()
	err := budget.run(ctx, func() error {
		defer close(finished)
		budget.start()
		<-blocked // Simulate cleanup that ignores cancellation.
		return nil
	})
	close(blocked)
	<-finished
	if err == nil || !strings.Contains(err.Error(), "total budget") {
		t.Fatalf("unbounded cleanup was not reported: %v", err)
	}
}
