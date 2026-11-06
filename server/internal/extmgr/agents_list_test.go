package extmgr

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/nwverse/persona/server/internal/agents"
)

// The unused source operations are never invoked by a list request.
type listAgentSource struct {
	agents.Source
	rows     []agents.Session
	calls    int
	searches int
}

func (s *listAgentSource) Name() string { return "demo" }
func (s *listAgentSource) Sessions(context.Context) ([]agents.Session, error) {
	s.calls++
	return s.rows, nil
}
func (s *listAgentSource) Search(context.Context, string) ([]agents.SearchHit, error) {
	s.searches++
	return []agents.SearchHit{}, nil
}
func (s *listAgentSource) Status(context.Context) (agents.Status, error) {
	return agents.Status{Source: "demo", Enabled: true}, nil
}

func TestPersonalAgentPagedRoutes8000(t *testing.T) {
	h, hd, _ := agentsHub(t)
	s := &listAgentSource{}
	for i := range 8000 {
		s.rows = append(s.rows, agents.Wrap("demo", agents.AgentSession{SessionKey: fmt.Sprintf("s%04d", i), Title: fmt.Sprintf("Demo %04d", i), AccountID: "account", Provider: "demo-provider", RankTimeMs: int64(i), UnreadCount: i % 2}, ""))
	}
	h.agents.Add(s)
	code, first := do(t, hd, "GET", "/api/v1/agents/sessions?summary=1&limit=50", "")
	if code != 200 || len(first["sessions"].([]any)) != 50 || first["total"].(float64) != 8000 {
		t.Fatalf("first: %d %v", code, first)
	}
	cursor := first["next_cursor"].(string)
	code, second := do(t, hd, "GET", "/api/v1/agents/sessions?summary=1&limit=50&cursor="+url.QueryEscape(cursor), "")
	if code != 200 || len(second["sessions"].([]any)) != 50 || s.calls != 1 {
		t.Fatalf("cached page: %d calls=%d", code, s.calls)
	}
	for _, flag := range []string{"1", "true"} {
		code, counts := do(t, hd, "GET", "/api/v1/agents/sessions?counts="+flag+"&state=unread&provider=demo-provider", "")
		if code != 200 || counts["total"].(float64) != 4000 || s.calls != 1 {
			t.Fatalf("counts: %d %v calls=%d", code, counts, s.calls)
		}
	}
	// Legacy q still searches messages and returns items; indexed q filters rows.
	code, legacy := do(t, hd, "GET", "/api/v1/agents/sessions?q=Demo", "")
	if code != 200 || legacy["items"] == nil || s.searches != 1 {
		t.Fatalf("legacy search: %d %v", code, legacy)
	}
	code, filtered := do(t, hd, "GET", "/api/v1/agents/sessions?summary=1&q=Demo+7999&group=account", "")
	if code != 200 || filtered["total"].(float64) != 1 || s.searches != 1 {
		t.Fatalf("filtered: %d %v", code, filtered)
	}
	rev := first["revision"].(string)
	s.rows = s.rows[:7999]
	h.agents.Updated("demo", nil)
	code, delta := do(t, hd, "GET", "/api/v1/agents/sessions?summary=1&since="+url.QueryEscape(rev), "")
	if code != 200 || len(delta["remove"].([]any)) != 1 || delta["remove"].([]any)[0] != "demo:s7999" {
		t.Fatalf("delta: %d %v", code, delta)
	}
	h.agents.Remove("demo")
	code, empty := do(t, hd, "GET", "/api/v1/agents/sessions?counts=1", "")
	if code != 200 || empty["total"].(float64) != 0 {
		t.Fatalf("remove source: %d %v", code, empty)
	}
}

func TestPersonalAgentListErrorsAreLocalized(t *testing.T) {
	_, hd, _ := agentsHub(t)
	for _, query := range []string{"limit=0", "limit=201", "counts=maybe", "cursor=", "since=broken", "state=broken"} {
		code, out := do(t, hd, "GET", "/api/v1/agents/sessions?"+query, "")
		if code != 400 || out["code"] != "invalid_query" {
			t.Fatalf("%s: %d %v", query, code, out)
		}
	}
}
