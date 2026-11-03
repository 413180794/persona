package node

import (
	"context"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
)

type fakeMetadataHost struct {
	mu          sync.Mutex
	agents      []host.Agent
	reportCalls []map[string]any
}

func (f *fakeMetadataHost) Kind() string                       { return host.Herdr }
func (f *fakeMetadataHost) Version(ctx context.Context) string { return "0.9.5" }
func (f *fakeMetadataHost) Snapshot(ctx context.Context) (*host.Layout, error) {
	return &host.Layout{}, nil
}
func (f *fakeMetadataHost) Agents(ctx context.Context) ([]host.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := make([]host.Agent, len(f.agents))
	for i, a := range f.agents {
		res[i] = a
		if a.Tokens != nil {
			res[i].Tokens = make(map[string]string, len(a.Tokens))
			for k, v := range a.Tokens {
				res[i].Tokens[k] = v
			}
		}
	}
	return res, nil
}
func (f *fakeMetadataHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	return nil, nil
}
func (f *fakeMetadataHost) Read(ctx context.Context, pane host.PaneRef, opts host.ReadOpts) (string, error) {
	return "", nil
}
func (f *fakeMetadataHost) SendText(ctx context.Context, pane host.PaneRef, text string) error {
	return nil
}
func (f *fakeMetadataHost) SendKeys(ctx context.Context, pane host.PaneRef, keys ...string) error {
	return nil
}
func (f *fakeMetadataHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	return nil
}
func (f *fakeMetadataHost) PaneProcesses(ctx context.Context, pane host.PaneRef) ([]int, error) {
	return nil, nil
}
func (f *fakeMetadataHost) PaneEnv() string { return "HERDR_PANE_ID" }

func (f *fakeMetadataHost) ReportMetadata(ctx context.Context, pane host.PaneRef, source string, tokens map[string]*string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reportCalls = append(f.reportCalls, map[string]any{
		"pane":   pane.ID,
		"source": source,
		"tokens": tokens,
	})
	for i := range f.agents {
		if f.agents[i].PaneID == pane.ID {
			if f.agents[i].Tokens == nil {
				f.agents[i].Tokens = make(map[string]string)
			}
			for k, v := range tokens {
				if v != nil {
					f.agents[i].Tokens[k] = *v
				} else {
					delete(f.agents[i].Tokens, k)
				}
			}
		}
	}
	return nil
}

func TestNodeTodoTokensReconciliation(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p1",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "s1",
				},
				Tokens: map[string]string{
					"role":         "leader",
					"persona_todo": "old_todo",
				},
			},
			{
				PaneID: "w1:p2",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "s2",
				},
				Tokens: map[string]string{
					"role":         "worker",
					"persona_from": "todo_1@m1/s1",
				},
			},
			{
				PaneID: "w1:p3",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "s3",
				},
				Tokens: map[string]string{
					"persona_from": "todo_1@m1/s1", // child not yet linked
				},
			},
		},
	}

	n := &Node{
		hosts: host.NewRegistry(fh),
	}

	ctx := context.Background()

	// 1. Sync expected tokens (keyed by session ID):
	// s1 expected: new title, todo_ref
	// s2 expected: todo_ref and todo title (active link, should consume persona_from)
	// s3 expected: none yet (persona_from should NOT be cleared yet)
	expected := map[string]map[string]string{
		"s1": {
			"persona_todo":     "new_todo",
			"persona_todo_ref": "todo_1@m1/s1",
		},
		"s2": {
			"persona_todo":     "new_todo",
			"persona_todo_ref": "todo_1@m1/s2",
		},
	}

	if err := n.syncTodoTokens(ctx, expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	agents, _ := fh.Agents(ctx)
	agentMap := make(map[string]map[string]string)
	for _, a := range agents {
		agentMap[a.PaneID] = a.Tokens
	}

	// Case 1: add missing tokens, update changed ones (w1:p1 persona_todo updated, persona_todo_ref added)
	p1Tokens := agentMap["w1:p1"]
	if p1Tokens["persona_todo"] != "new_todo" || p1Tokens["persona_todo_ref"] != "todo_1@m1/s1" {
		t.Fatalf("w1:p1 tokens mismatch: %+v", p1Tokens)
	}
	// Case 2: do not touch other tokens ("role" still "leader")
	if p1Tokens["role"] != "leader" {
		t.Fatalf("w1:p1 other token 'role' was modified: %+v", p1Tokens)
	}

	// Case 3: consume persona_from (w1:p2 has active link, persona_from cleared)
	p2Tokens := agentMap["w1:p2"]
	if _, ok := p2Tokens["persona_from"]; ok {
		t.Fatalf("w1:p2 expected persona_from to be consumed and cleared, got: %+v", p2Tokens)
	}
	if p2Tokens["persona_todo"] != "new_todo" || p2Tokens["persona_todo_ref"] != "todo_1@m1/s2" {
		t.Fatalf("w1:p2 tokens mismatch: %+v", p2Tokens)
	}

	// w1:p3 has no active link yet, persona_from preserved
	p3Tokens := agentMap["w1:p3"]
	if p3Tokens["persona_from"] != "todo_1@m1/s1" {
		t.Fatalf("w1:p3 expected persona_from preserved, got: %+v", p3Tokens)
	}

	// Case 4: tokens restored after herdr restart wipes them
	fh.mu.Lock()
	for i := range fh.agents {
		// Simulating herdr restart: all custom tokens wiped, only base host tokens (role) remain
		cleanTokens := make(map[string]string)
		if r, ok := fh.agents[i].Tokens["role"]; ok {
			cleanTokens["role"] = r
		}
		fh.agents[i].Tokens = cleanTokens
	}
	fh.mu.Unlock()

	// Reconcile again (as background timer would do)
	if err := n.reconcileTodoTokens(ctx); err != nil {
		t.Fatalf("reconcileTodoTokens after restart: %v", err)
	}

	agents, _ = fh.Agents(ctx)
	agentMap = make(map[string]map[string]string)
	for _, a := range agents {
		agentMap[a.PaneID] = a.Tokens
	}

	p1Recovered := agentMap["w1:p1"]
	if p1Recovered["persona_todo"] != "new_todo" || p1Recovered["persona_todo_ref"] != "todo_1@m1/s1" {
		t.Fatalf("w1:p1 failed to recover tokens after restart: %+v", p1Recovered)
	}
	p2Recovered := agentMap["w1:p2"]
	if p2Recovered["persona_todo"] != "new_todo" || p2Recovered["persona_todo_ref"] != "todo_1@m1/s2" {
		t.Fatalf("w1:p2 failed to recover tokens after restart: %+v", p2Recovered)
	}
}

// TestNodeTodoTokensPaneIDChanged verifies that when pane ID changes (e.g. after herdr restart),
// tokens are written to the new pane and cleared from the old pane, never remaining on the old pane (#873).
func TestNodeTodoTokensPaneIDChanged(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p1",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "s1",
				},
				Tokens: map[string]string{},
			},
		},
	}

	n := &Node{
		hosts: host.NewRegistry(fh),
	}

	ctx := context.Background()

	// 1. Sync expected tokens for session s1
	expected := map[string]map[string]string{
		"s1": {
			"persona_todo":     "todo_title",
			"persona_todo_ref": "todo_1@m1/s1",
		},
	}
	if err := n.syncTodoTokens(ctx, expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	// Verify w1:p1 got tokens
	agents, _ := fh.Agents(ctx)
	if agents[0].Tokens["persona_todo"] != "todo_title" {
		t.Fatalf("expected w1:p1 to have todo_title, got: %+v", agents[0].Tokens)
	}

	// 2. Simulate herdr restart: pane id changes from w1:p1 to w2:p5
	// Suppose w1:p1 was left with stale tokens on another process, while s1 is now running on w2:p5
	fh.mu.Lock()
	fh.agents = []host.Agent{
		{
			PaneID: "w1:p1", // old pane
			Tokens: map[string]string{
				"persona_todo":     "todo_title",
				"persona_todo_ref": "todo_1@m1/s1",
			},
		},
		{
			PaneID: "w2:p5", // new pane where s1 now runs
			AgentSession: &host.AgentSession{
				Kind:  "id",
				Value: "s1",
			},
			Tokens: map[string]string{},
		},
	}
	fh.mu.Unlock()

	// 3. Reconcile
	if err := n.reconcileTodoTokens(ctx); err != nil {
		t.Fatalf("reconcileTodoTokens: %v", err)
	}

	// 4. Verify tokens on live agents:
	// New pane w2:p5 has the tokens; Old pane w1:p1 has tokens cleared!
	agents, _ = fh.Agents(ctx)
	agentMap := make(map[string]map[string]string)
	for _, a := range agents {
		agentMap[a.PaneID] = a.Tokens
	}

	oldPaneTokens := agentMap["w1:p1"]
	if _, ok := oldPaneTokens["persona_todo"]; ok {
		t.Fatalf("expected old pane w1:p1 persona_todo to be cleared, got: %+v", oldPaneTokens)
	}
	if _, ok := oldPaneTokens["persona_todo_ref"]; ok {
		t.Fatalf("expected old pane w1:p1 persona_todo_ref to be cleared, got: %+v", oldPaneTokens)
	}

	newPaneTokens := agentMap["w2:p5"]
	if newPaneTokens["persona_todo"] != "todo_title" || newPaneTokens["persona_todo_ref"] != "todo_1@m1/s1" {
		t.Fatalf("expected new pane w2:p5 to have tokens, got: %+v", newPaneTokens)
	}
}

// TestNodeTodoTokensDeadSessionDoesNotPolluteLivePane verifies that when lastPanes contains a stale
// mapping from dead session sA to a pane currently occupied by live session sB, and expected tokens
// only contain sA, sA's tokens are NOT written to the pane (#892 review).
func TestNodeTodoTokensDeadSessionDoesNotPolluteLivePane(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p1",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "sB", // live agent in w1:p1 is sB (e.g. after claude /clear or restart)
				},
				Tokens: map[string]string{},
			},
		},
	}

	n := &Node{
		hosts: host.NewRegistry(fh),
		lastPanes: map[string]string{
			"sA": "w1:p1", // sA was historically in w1:p1
			"sB": "w1:p1", // sB is currently in w1:p1
		},
	}

	ctx := context.Background()

	// Expected tokens only has sA (which is dead/offline), not sB.
	expected := map[string]map[string]string{
		"sA": {
			"persona_todo":     "dead_todo",
			"persona_todo_ref": "td_dead@m1/sA",
		},
	}

	if err := n.syncTodoTokens(ctx, expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	// Verify that w1:p1 did NOT get sA's tokens
	agents, _ := fh.Agents(ctx)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if tok, ok := agents[0].Tokens["persona_todo"]; ok {
		t.Fatalf("pane w1:p1 unexpectedly received dead session sA's persona_todo: %q", tok)
	}
	if tok, ok := agents[0].Tokens["persona_todo_ref"]; ok {
		t.Fatalf("pane w1:p1 unexpectedly received dead session sA's persona_todo_ref: %q", tok)
	}
}

func TestNodeReconcileDoesNotClearTokensBeforeFirstSync(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p1",
				Tokens: map[string]string{
					"persona_todo":     "todo_retain_test",
					"persona_todo_ref": "todo_1@m1/s1",
					"role":             "worker",
				},
			},
		},
	}

	n := &Node{
		hosts: host.NewRegistry(fh),
	}

	ctx := context.Background()

	// 1. Before any syncTodoTokens call, reconcile should be read-only and NOT clear tokens
	if err := n.reconcileTodoTokens(ctx); err != nil {
		t.Fatalf("reconcileTodoTokens before sync: %v", err)
	}

	agents, _ := fh.Agents(ctx)
	if len(agents) != 1 || agents[0].Tokens["persona_todo"] != "todo_retain_test" {
		t.Fatalf("expected tokens untouched before first sync, got: %+v", agents[0].Tokens)
	}

	// 2. Once syncTodoTokens is called (even with empty map), it should now clear obsolete tokens
	if err := n.syncTodoTokens(ctx, map[string]map[string]string{}); err != nil {
		t.Fatalf("syncTodoTokens empty: %v", err)
	}

	agents, _ = fh.Agents(ctx)
	if _, ok := agents[0].Tokens["persona_todo"]; ok {
		t.Fatalf("expected persona_todo to be cleared after empty sync, got: %+v", agents[0].Tokens)
	}
	if agents[0].Tokens["role"] != "worker" {
		t.Fatalf("role token modified: %+v", agents[0].Tokens)
	}
}

func TestNodeReconcileSkipsNonHerdrPanes(t *testing.T) {
	// The host lists a pane whose id names another host (Orca), with a stale persona_ mark on it: reconciling reaches
	// the host-kind check for it, and nothing is written to it through the herdr reporter (#948).
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p1",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "s_herdr",
				},
				Tokens: map[string]string{},
			},
			{
				PaneID: "orca:t1:p1",
				Tokens: map[string]string{"persona_todo": "stale"},
			},
		},
	}
	n := &Node{
		hosts: host.NewRegistry(fh),
		agents: map[string]host.Agent{
			"s_herdr": {PaneID: "w1:p1"},
			"s_orca":  {PaneID: "orca:t1:p1"},
		},
	}
	ctx := context.Background()

	// Expected contains an orca session and a herdr session
	expected := map[string]map[string]string{
		"s_orca": {
			"persona_todo":     "orca_todo",
			"persona_todo_ref": "todo_1@m1/s_orca",
		},
		"s_herdr": {
			"persona_todo":     "herdr_todo",
			"persona_todo_ref": "todo_1@m1/s_herdr",
		},
	}

	if err := n.syncTodoTokens(ctx, expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	fh.mu.Lock()
	defer fh.mu.Unlock()
	herdrWritten := false
	for _, call := range fh.reportCalls {
		if call["pane"] == "orca:t1:p1" || call["pane"] == "t1:p1" {
			t.Fatalf("herdr reporter should not receive report for orca pane: %+v", call)
		}
		if call["pane"] == "w1:p1" {
			herdrWritten = true
		}
	}
	if !herdrWritten {
		t.Fatalf("the herdr pane's marks were not written: %+v", fh.reportCalls)
	}
}

// TestNodeReconcileSameSessionInTwoPanes: a session open in two panes (resumed in a second one) keeps its marks on
// the pane n.agents has for it; the other pane, which the host reports with the same session id, gets none (#948).
func TestNodeReconcileSameSessionInTwoPanes(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{PaneID: "w1:p1", AgentSession: &host.AgentSession{Kind: "id", Value: "s1"}, Tokens: map[string]string{}},
			{PaneID: "w1:p2", AgentSession: &host.AgentSession{Kind: "id", Value: "s1"}, Tokens: map[string]string{}},
		},
	}
	n := &Node{
		hosts:  host.NewRegistry(fh),
		agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}},
	}
	expected := map[string]map[string]string{
		"s1": {"persona_todo": "todo_a", "persona_todo_ref": "todo_a@m1/s1"},
	}
	if err := n.syncTodoTokens(context.Background(), expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	fh.mu.Lock()
	defer fh.mu.Unlock()
	var panes []any
	for _, call := range fh.reportCalls {
		panes = append(panes, call["pane"])
	}
	if len(panes) != 1 || panes[0] != "w1:p1" {
		t.Fatalf("marks written to %v, want only w1:p1", panes)
	}
	if got := fh.agents[1].Tokens["persona_todo"]; got != "" {
		t.Fatalf("second pane got persona_todo %q", got)
	}
}

// TestNodeTodoTokensFreshPaneNilTokens verifies that when a live pane has Tokens==nil (fresh pane in herdr),
// and n.agents tracks the session, expected tokens are still successfully written on first sync (#892).
func TestNodeTodoTokensFreshPaneNilTokens(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p1",
				Tokens: nil, // herdr returns no tokens key for a fresh pane with no metadata
			},
		},
	}

	n := &Node{
		hosts: host.NewRegistry(fh),
		agents: map[string]host.Agent{
			"s1": {PaneID: "w1:p1"},
		},
	}

	ctx := context.Background()

	expected := map[string]map[string]string{
		"s1": {
			"persona_todo":     "fresh_todo",
			"persona_todo_ref": "td_fresh@m1/s1",
		},
	}

	if err := n.syncTodoTokens(ctx, expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	agents, _ := fh.Agents(ctx)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Tokens["persona_todo"] != "fresh_todo" || agents[0].Tokens["persona_todo_ref"] != "td_fresh@m1/s1" {
		t.Fatalf("fresh pane failed to receive tokens: %+v", agents[0].Tokens)
	}
}

// TestNodeTodoTokensCodexPublicIDMismatch verifies that when a Codex pane has a raw thread ID in AgentSession
// but the node public session ID (in n.agents) is different (e.g. c0de-...), tokens are resolved by public ID (#892).
func TestNodeTodoTokensCodexPublicIDMismatch(t *testing.T) {
	fh := &fakeMetadataHost{
		agents: []host.Agent{
			{
				PaneID: "w1:p2",
				AgentSession: &host.AgentSession{
					Kind:  "id",
					Value: "019a-raw-thread-uuid", // herdr's raw thread ID
				},
				Tokens: map[string]string{},
			},
		},
	}

	n := &Node{
		hosts: host.NewRegistry(fh),
		agents: map[string]host.Agent{
			"c0de-deadbeef1234": {PaneID: "w1:p2"}, // node's public session ID known to hub
		},
	}

	ctx := context.Background()

	// Hub keys expected tokens by the public session ID "c0de-deadbeef1234"
	expected := map[string]map[string]string{
		"c0de-deadbeef1234": {
			"persona_todo":     "codex_todo",
			"persona_todo_ref": "td_cx@m1/c0de-deadbeef1234",
		},
	}

	if err := n.syncTodoTokens(ctx, expected); err != nil {
		t.Fatalf("syncTodoTokens: %v", err)
	}

	agents, _ := fh.Agents(ctx)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Tokens["persona_todo"] != "codex_todo" || agents[0].Tokens["persona_todo_ref"] != "td_cx@m1/c0de-deadbeef1234" {
		t.Fatalf("codex pane failed to receive tokens via public ID: %+v", agents[0].Tokens)
	}
}
