package hub

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// autoTabName is a short name for the herdr tab of a new agent, from its first message (the small-task model, a few
// seconds at most). "" when there is no message or the model fails: the node names it agent-<n>.
func (h *Hub) autoTabName(ctx context.Context, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if r := []rune(prompt); len(r) > 1500 {
		prompt = string(r[:1500])
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := h.purposeModel("agent.title").Complete(cctx, prompts.RenderTabName(), prompt, 200) // reasoning models think first: 20 came back empty
	if err != nil {
		if !ai.IsPaused(err) {
			h.logf("tab name: %v", err)
		}
		return ""
	}
	name := tabSlug(out)
	if name == "" {
		h.logf("tab name: nothing usable in %q", clip(oneLine(out), 80))
	}
	return name
}

// tabSlug keeps what a tab name may be: lowercase ASCII letters and digits, single hyphens, at most 24 characters.
func tabSlug(s string) string {
	if i := strings.IndexByte(strings.TrimSpace(s), '\n'); i > 0 {
		s = strings.TrimSpace(s)[:i]
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '/':
			dash = true
		}
		if b.Len() >= 24 {
			break
		}
	}
	return strings.Trim(b.String()[:min(b.Len(), 24)], "-")
}

// defaultTabName: a name nobody chose — the old fixed "jarvis", herdr's numbers, the agent-<n> fallback.
var defaultTabName = regexp.MustCompile(`^(jarvis(-\d+)?|\d+|agent-\d+)?$`)

// TabRename is one tab of POST /api/v1/tabs/autoname: renamed, or (dry run) what it would be called.
type TabRename struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	Space     string `json:"space"`
	Title     string `json:"title"`
	From      string `json:"from"`
	To        string `json:"to,omitempty"`
	Error     string `json:"error,omitempty"`
}

// autonameTabs renames the herdr tabs of running agents whose names nobody chose (all of them with all) after
// their session's title, the way new tabs are named. One tab per agent; agents without a title keep theirs.
func (h *Hub) autonameTabs(ctx context.Context, dryRun, all bool) []TabRename {
	type tab struct {
		m *Machine
		s proto.Session
	}
	var todo []tab
	taken := map[string]map[string]bool{} // machine/workspace → names in use
	for _, m := range h.machines {
		seen := map[string]bool{}
		for _, s := range h.decorated(m) {
			if s.Assistant || s.SpaceKind != "herdr" || s.TabID == "" || s.Status == proto.StatusOffline || seen[s.TabID] {
				continue
			}
			seen[s.TabID] = true
			ws := m.cfg.ID + "/" + s.SpaceID
			if taken[ws] == nil {
				taken[ws] = map[string]bool{}
			}
			taken[ws][s.SlotName] = true
			if (all || defaultTabName.MatchString(s.SlotName)) && strings.TrimSpace(s.Title) != "" {
				todo = append(todo, tab{m, s})
			}
		}
	}
	// Names first (the model, a few at a time), then pick unique ones per space in a fixed order.
	names := make([]string, len(todo))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, t := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if names[i] = h.autoTabName(ctx, t.s.Title); names[i] == "" { // the small model comes back empty now and then
				names[i] = h.autoTabName(ctx, t.s.Title)
			}
		}()
	}
	wg.Wait()
	out := make([]TabRename, 0, len(todo))
	for i, t := range todo {
		r := TabRename{MachineID: t.m.cfg.ID, SessionID: t.s.SessionID, Space: t.s.SpaceName, Title: t.s.Title, From: t.s.SlotName}
		if names[i] == "" {
			r.Error = "no name from the model"
			out = append(out, r)
			continue
		}
		ws := taken[t.m.cfg.ID+"/"+t.s.SpaceID]
		delete(ws, t.s.SlotName) // its own old name is free again
		r.To = uniqueName(names[i], ws)
		ws[r.To] = true
		if !dryRun && r.To != r.From {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := t.m.call(cctx, "session.rename", map[string]any{"session_id": t.s.SessionID, "tab_name": r.To}, nil); err != nil {
				r.Error = err.Error()
			}
			cancel()
		}
		out = append(out, r)
	}
	return out
}

// uniqueName is name, or name-2, name-3… when taken.
func uniqueName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s-%d", name, i); !taken[n] {
			return n
		}
	}
}
