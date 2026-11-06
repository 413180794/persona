package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestRuntimeList8000OverHTTPAndSocket(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo", Kind: "local"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	for i := range 8000 {
		m.sessions = append(m.sessions, proto.Session{MachineID: "demo", SessionID: fmt.Sprintf("s%04d", i),
			Title: "Demo task", State: "idle", Status: proto.StatusIdle,
			UpdatedAt: time.Unix(int64(i), 0), LastText: strings.Repeat("demo preview ", 50)})
	}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/ws?features=sessions_list,sessions_delta",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.cfg.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20) // a mistakenly delivered full 8,000-row snapshot must fail
	readNotice := func() string {
		t.Helper()
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Type, Revision string
				Features       []string
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type == "hello" && !slices.Contains(event.Features, featureSessionsLookup) {
				t.Fatal("hello did not advertise exact session lookup")
			}
			if event.Type == "sessions" || event.Type == "sessions_delta" {
				t.Fatalf("unexpected unbounded list: %s", event.Type)
			}
			if event.Type == "sessions_list" {
				if len(raw) > 160 || event.Revision == "" {
					t.Fatalf("notice: %s", raw)
				}
				t.Logf("8000-session WebSocket notice: %d bytes", len(raw))
				return event.Revision
			}
		}
	}
	_ = readNotice()
	get := func(query string) listTestPage {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/sessions?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var page listTestPage
		if res.StatusCode != 200 {
			t.Fatalf("HTTP %d", res.StatusCode)
		}
		if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	page := get("summary=1&limit=50")
	if len(page.Sessions) != 50 || page.Total != 8000 || !page.HasMore {
		t.Fatalf("first: %+v", page)
	}
	m.mu.Lock()
	m.sessions[7999].Title = "Renamed demo task"
	m.mu.Unlock()
	h.sessionsChanged(m)
	_ = readNotice()
	delta := get("summary=1&limit=50&since=" + url.QueryEscape(page.Revision))
	if len(delta.Sessions) != 1 || delta.Sessions[0].Title != "Renamed demo task" || delta.Total != 8000 {
		t.Fatalf("delta: %+v", delta)
	}
}
