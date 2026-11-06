package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/proto"
)

// Classification must survive both HTTP and WebSocket snapshots, not just the node's terminal guard.
func TestAssistantClassificationAcrossSessionLists(t *testing.T) {
	h, n := conversationsHub(t)
	n.m.setSessions([]proto.Session{
		{SessionID: "pane", Cwd: convDir, PaneID: "w1:p1", Status: proto.StatusIdle, Title: "terminal title"},
		{SessionID: "symlink-pane", Cwd: "/data/home/me/.persona/assistant", PaneID: "w1:p2", Status: proto.StatusIdle},
		{SessionID: "headless", Cwd: convDir, Status: proto.StatusWorking},
		{SessionID: "runtime", Cwd: convDir, SourceKind: "runtime", Status: proto.StatusWorking},
		{SessionID: "offline-assistant", Cwd: convDir, Status: proto.StatusOffline},
		{SessionID: "ordinary", Cwd: "/code/app", PaneID: "w1:p3", Status: proto.StatusIdle},
	})
	want := map[string]bool{"pane": false, "symlink-pane": false, "headless": true, "runtime": true, "offline-assistant": true, "ordinary": false}
	check := func(label string, rows []proto.Session) {
		t.Helper()
		if len(rows) != len(want) {
			t.Fatalf("%s: got %d sessions, want %d", label, len(rows), len(want))
		}
		for _, s := range rows {
			w, ok := want[s.SessionID]
			if !ok || s.Assistant != w {
				t.Errorf("%s: %s assistant=%v, want %v", label, s.SessionID, s.Assistant, w)
			}
		}
	}
	check("decorated", h.decorated(n.m))
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list pushed
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("HTTP list: status=%d error=%v", res.StatusCode, err)
	}
	check("HTTP", list.Sessions)
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/assistant", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	resAssistant, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resAssistant.Body.Close()
	var assistant assistantView
	if err := json.NewDecoder(resAssistant.Body).Decode(&assistant); err != nil || resAssistant.StatusCode != http.StatusOK {
		t.Fatalf("assistant list: status=%d error=%v", resAssistant.StatusCode, err)
	}
	if len(assistant.Conversations) != 3 {
		t.Errorf("assistant list has %d conversations, want only the three headless sessions", len(assistant.Conversations))
	}
	for _, s := range assistant.Conversations {
		if !want[s.SessionID] || s.PaneID != "" {
			t.Errorf("host pane leaked into assistant list: %s", s.SessionID)
		}
	}
	for _, query := range []string{"", "?features=sessions_delta"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/ws"+query,
			&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.cfg.Token}}})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		c.SetReadLimit(1 << 20)
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var p pushed
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatal(err)
			}
			if p.Type == "sessions" && p.Machine == n.m.cfg.ID {
				check("WebSocket"+query, p.Sessions)
				break
			}
		}
		_ = c.CloseNow()
		cancel()
	}
}
