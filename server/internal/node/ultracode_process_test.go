package node

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

const ultraProcessTestID = "abcdef01-0000-4000-8000-000000000001"

func ultracodeProcessNode(t *testing.T) (*Node, string) {
	t.Helper()
	n, home := testNode(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	path := filepath.Join(home, "session.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n.paths[ultraProcessTestID] = path
	n.agents[ultraProcessTestID] = host.Agent{Agent: "claude", PaneID: "test-pane"}
	return n, path
}

func TestUltracodeBackgroundScanRetainsEarlyState(t *testing.T) {
	n, path := ultracodeProcessNode(t)
	on := []byte(`{"type":"attachment","timestamp":"2026-01-01T08:00:00Z","attachment":{"type":"ultra_effort_enter","reminderType":"full"}}` + "\n")
	filler := bytes.Repeat([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`+"\n"), quickTail/64)
	if err := os.WriteFile(path, append(on, filler...), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() <= quickTail {
		t.Fatal("fixture must place the toggle before the quick tail")
	}
	if tail := readQuickMeta(path, st); tail.ultracode != nil {
		t.Fatal("fixture accidentally left evidence in the head/tail read")
	}
	m := n.meta(path, st)
	if m.ultracode == nil || !*m.ultracode || m.ultracodeAt.Format(time.RFC3339) != "2026-01-01T08:00:00Z" {
		t.Fatalf("cold read lost early evidence: %+v", m)
	}
	appendLine := func(line string) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(line + "\n")
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		n.backgroundTasks(path)
	}
	appendLine(`{"type":"attachment","timestamp":"2026-01-01T09:00:00Z","attachment":{"type":"ultra_effort_exit"}}`)
	if value, at := n.bgUltracode(path); value == nil || *value || at.Hour() != 9 {
		t.Fatalf("appended off was not observed: %v at %v", value, at)
	}
	appendLine(`{"type":"attachment","timestamp":"2026-01-01T10:00:00Z","isSidechain":true,"attachment":{"type":"ultra_effort_enter","reminderType":"full"}}`)
	appendLine(`{"type":"attachment","timestamp":"2026-01-01T08:30:00Z","attachment":{"type":"ultra_effort_enter","reminderType":"sparse"}}`)
	if value, at := n.bgUltracode(path); value == nil || *value || at.Hour() != 9 {
		t.Fatalf("sidechain or older evidence replaced off: %v at %v", value, at)
	}
}

func TestUltracodeProcessResumeAndStateReload(t *testing.T) {
	n, _ := ultracodeProcessNode(t)
	before := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	started := before.Add(time.Hour)
	n.state.put(ultraProcessTestID, sessionState{Launcher: "claude", Cwd: "/work/project", Model: "opus", StartedAt: before}) // Persona's
	n.mu.Lock()
	u := n.ultracodeLocked(ultraProcessTestID)
	u.Value, u.At = ultraBool(true), before
	u.pending, u.note, u.rejectedModel = ultraBool(true), "old refusal", "old-model"
	n.mu.Unlock()
	n.beginUltracodeProcess(ultraProcessTestID, true, true, started)
	if u.Value == nil || *u.Value || !u.Since.Equal(started) || !u.At.Equal(started) || u.LostAt == nil || !u.LostAt.Equal(started) {
		t.Fatalf("resume must be off with a loss marker: %+v", u)
	}
	if u.pending != nil || u.note != "" || u.rejectedModel != "" {
		t.Fatalf("resume kept a requested value or rejection: %+v", u)
	}
	st, ok := n.state.get(ultraProcessTestID)
	if !ok || st.Ultracode == nil {
		t.Fatal("resume boundary was not persisted")
	}
	if st.Launcher != "claude" || st.Cwd != "/work/project" || st.Model != "opus" || !st.StartedAt.Equal(before) {
		t.Fatalf("writing the observation changed the rest of the record: %+v", st)
	}
	encoded, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var loaded sessionState
	if err := json.Unmarshal(encoded, &loaded); err != nil {
		t.Fatal(err)
	}
	restarted, _ := ultracodeProcessNode(t)
	restarted.state.merge(map[string]sessionState{ultraProcessTestID: loaded})
	restarted.mu.Lock()
	after := restarted.ultracodeLocked(ultraProcessTestID)
	restarted.mu.Unlock()
	after.observe(quickMeta{ultracode: ultraBool(true), ultracodeAt: before})
	if after.Value == nil || *after.Value || !after.Since.Equal(started) || after.LostAt == nil || !after.LostAt.Equal(started) {
		t.Fatalf("node reload let old history re-enable ultracode: %+v", after)
	}
	after.observe(quickMeta{ultracode: ultraBool(true), ultracodeAt: started.Add(time.Minute)})
	if after.Value == nil || !*after.Value || after.LostAt != nil {
		t.Fatalf("confirmed re-enable did not clear the loss marker: %+v", after)
	}
}

func TestUltracodeHookProcessBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, event, source, agentID string
		reconnect                    bool
	}{
		{"clear", "SessionStart", "clear", "", false},
		{"compact", "SessionStart", "compact", "", false},
		{"hook reconnect", "SessionStart", "resume", "", true},
		{"sidechain", "SessionStart", "resume", "child", false},
		{"other hook", "UserPromptSubmit", "resume", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n, path := ultracodeProcessNode(t)
			started := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
			n.mu.Lock()
			u := n.ultracodeLocked(ultraProcessTestID)
			u.Value, u.At, u.Since = ultraBool(true), started, started
			n.mu.Unlock()
			n.ultracodeHook(sockRequest{Resume: tt.reconnect}, hookInput{SessionID: ultraProcessTestID, TranscriptPath: path,
				HookEventName: tt.event, Source: tt.source, AgentID: tt.agentID})
			if u.Value == nil || !*u.Value || !u.Since.Equal(started) || u.LostAt != nil {
				t.Fatalf("non-process hook reset ultracode: %+v", u)
			}
		})
	}
	for _, source := range []string{"startup", "resume"} {
		t.Run(source, func(t *testing.T) {
			n, path := ultracodeProcessNode(t)
			n.mu.Lock()
			u := n.ultracodeLocked(ultraProcessTestID)
			u.Value, u.At = ultraBool(true), time.Now().Add(-time.Hour)
			n.mu.Unlock()
			before := time.Now()
			n.ultracodeHook(sockRequest{}, hookInput{SessionID: ultraProcessTestID, TranscriptPath: path, HookEventName: "SessionStart", Source: source})
			if u.Since.Before(before) || u.Since.After(time.Now()) || u.LostAt == nil {
				t.Fatalf("new process did not establish a boundary: %+v", u)
			}
			if source == "resume" && (u.Value == nil || *u.Value) {
				t.Fatalf("resume did not confirm off: %+v", u)
			}
			if source == "startup" && u.Value != nil {
				t.Fatalf("fresh startup invented a state before evidence: %+v", u)
			}
		})
	}
}

func TestUltracodeOwnStartHookKeepsConfirmedChoice(t *testing.T) {
	n, path := ultracodeProcessNode(t)
	started := time.Now().Add(-time.Second)
	n.beginUltracodeProcess(ultraProcessTestID, false, true, started)
	n.mu.Lock()
	u := n.ultracodeLocked(ultraProcessTestID)
	u.Value, u.At = ultraBool(true), started.Add(time.Millisecond)
	n.mu.Unlock()
	n.ultracodeHook(sockRequest{}, hookInput{SessionID: ultraProcessTestID, TranscriptPath: path, HookEventName: "SessionStart", Source: "startup"})
	if u.Value == nil || !*u.Value || !u.Since.Equal(started) || u.LostAt != nil || u.awaitingHook {
		t.Fatalf("late own-start hook erased its command verdict: %+v", u)
	}
}

func TestUltracodeListKeyTracksAllFields(t *testing.T) {
	at := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	later := at.Add(time.Nanosecond)
	variants := map[string]proto.Session{
		"unknown":    {},
		"off":        {Ultracode: ultraBool(false)},
		"on":         {Ultracode: ultraBool(true)},
		"note":       {UltracodeNote: "需要开启工作流"},
		"pending":    {UltracodePending: true},
		"lost":       {UltracodeLostAt: &at},
		"lost later": {UltracodeLostAt: &later},
	}
	seen := map[string]string{}
	for name, row := range variants {
		row.SessionID = ultraProcessTestID
		key := listKey([]proto.Session{row})
		if other, ok := seen[key]; ok {
			t.Fatalf("%s and %s have the same list key", name, other)
		}
		seen[key] = name
	}
}
