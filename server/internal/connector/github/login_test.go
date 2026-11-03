package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

type memSecrets struct {
	mu   sync.Mutex
	data []byte
}

func (m *memSecrets) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, nil
}
func (m *memSecrets) Save(_ context.Context, d []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = d
	return nil
}
func (m *memSecrets) Delete(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = nil
	return nil
}

// The device flow against a made-up GitHub: the client ID once, the code, waiting, slowing down, the token; it is
// kept sealed and used before a computer's gh login; signing out forgets it and keeps the client ID.
func TestDeviceLogin(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/login/device/code":
			if r.Form.Get("client_id") != "Ov23liExample" {
				_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
				return
			}
			if r.Form.Get("scope") != deviceScopes {
				t.Errorf("scope %q", r.Form.Get("scope"))
			}
			_, _ = w.Write([]byte(`{"device_code":"dc1","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`))
		case "/login/oauth/access_token":
			polls++
			switch polls {
			case 1:
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			case 2:
				_, _ = w.Write([]byte(`{"error":"slow_down","interval":10}`))
			default:
				_, _ = w.Write([]byte(`{"access_token":"gho_example","token_type":"bearer"}`))
			}
		case "/user":
			if r.Header.Get("Authorization") != "Bearer gho_example" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"login":"example-user"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	sec := &memSecrets{}
	c := New(Config{}, "", nil)
	c.web, c.api = srv.URL, srv.URL
	c.UseSecrets(sec)
	c.check(ctx)
	if st := c.Status(ctx); st.State != connector.StateNotSignedIn || st.Error != notSignedIn || st.Via != "" {
		t.Fatalf("before: %+v", st)
	}
	if err := connector.Check(c); err != nil {
		t.Fatal(err)
	}

	p, err := c.LoginStep(ctx, "", nil)
	if err != nil || p.Step != "client_id" || len(p.Fields) != 1 {
		t.Fatalf("first step %+v %v", p, err)
	}
	if p, _ = c.LoginStep(ctx, "client_id", map[string]string{"client_id": "wrong"}); p.Step != "client_id" || p.Error == "" {
		t.Fatalf("a client id GitHub refuses: %+v", p)
	}
	p, err = c.LoginStep(ctx, "client_id", map[string]string{"client_id": " Ov23liExample "})
	if err != nil || p.Kind != connector.PromptDeviceCode || p.UserCode != "ABCD-1234" || p.URL != "https://github.com/login/device" || p.Interval != 5 {
		t.Fatalf("device step %+v %v", p, err)
	}
	if p, _ = c.LoginStep(ctx, "device", nil); p.Step != "device" || p.Error != "" || p.Interval != 5 {
		t.Fatalf("pending %+v", p)
	}
	if p, _ = c.LoginStep(ctx, "device", nil); p.Interval != 10 {
		t.Fatalf("slow down %+v", p)
	}
	if p, err = c.LoginStep(ctx, "device", nil); err != nil || !p.Done || p.Account != "example-user" {
		t.Fatalf("done %+v %v", p, err)
	}
	if st := c.Status(ctx); st.State != connector.StateOK || st.Via != "设备码登录" {
		t.Fatalf("after: %+v", st)
	}
	var kept deviceLogin
	_ = json.Unmarshal(sec.data, &kept)
	if kept.ClientID != "Ov23liExample" || kept.Token != "gho_example" {
		t.Fatalf("kept %+v", kept)
	}

	// A hub started again signs in from what it kept.
	c2 := New(Config{}, "", nil)
	c2.web, c2.api = srv.URL, srv.URL
	c2.UseSecrets(sec)
	c2.check(ctx)
	if st := c2.Status(ctx); st.State != connector.StateOK {
		t.Fatalf("after a restart: %+v", st)
	}

	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	kept = deviceLogin{}
	_ = json.Unmarshal(sec.data, &kept)
	if kept.Token != "" || kept.ClientID != "Ov23liExample" {
		t.Fatalf("after logout %+v", kept)
	}
	if st := c.Status(ctx); st.State != connector.StateNotSignedIn {
		t.Fatalf("signed out: %+v", st)
	}
	// Signing in again starts at the code: the client ID is kept.
	if p, _ = c.LoginStep(ctx, "", nil); p.Kind != connector.PromptDeviceCode {
		t.Fatalf("again %+v", p)
	}
	if strings.Contains(string(sec.data), "dc1") {
		t.Fatal("the device code was kept")
	}
}
