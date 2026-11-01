package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Characterization tests of what the node does to herdr to start, prompt, key, close and rename sessions: the exact
// herdr calls, in order.

const startID = "0e000000-0000-4000-8000-0000000000aa"

// startHerdr answers agent.get for pane as a claude in status, and a snapshot with no agents.
func startHerdr(f *fakeHerdr, pane, status string) {
	f.reply("agent.get", map[string]any{"agent": map[string]any{"agent": "claude", "agent_status": status, "pane_id": pane,
		"tab_id": "tab-of-" + pane, "workspace_id": "ws-of-" + pane}})
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
}

func startCmd(n *Node, launcher, args string) string {
	return launcher + " " + args + " --settings " + shellQuote(filepath.Join(n.dir, "claude-settings.json")) +
		" --mcp-config " + shellQuote(filepath.Join(n.dir, "mcp.json"))
}

func readVisible(pane string) string {
	return call("pane.read", map[string]any{"pane_id": pane, "source": "visible", "lines": 60})
}

func agentGet(pane string) string { return call("agent.get", map[string]any{"target": pane}) }

// The workspace named after the directory is reused: a new tab in it (named agent-<n> past the taken ones), the
// launcher typed with the session id, mode and effort flags, the trust dialog accepted, then the agent renamed and
// the first message prompted.
func TestHerdrStartInTheProjectsWorkspace(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	f.reply("workspace.list", map[string]any{"workspaces": []any{
		map[string]any{"workspace_id": "w0", "label": "other"}, map[string]any{"workspace_id": "w1", "label": "proj"}}})
	startHerdr(f, "w1:p2", proto.StatusIdle)
	f.reply("session.snapshot", snapshot(nil, []any{
		map[string]any{"tab_id": "w1:t1", "workspace_id": "w1", "label": "agent-1"},
		map[string]any{"tab_id": "w0:t1", "workspace_id": "w0", "label": "agent-2"}, // another workspace's: free here
	}, nil, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.screenSteps("w1:p2", isKeys(), screenFixture(t, "claude-2.1.312-trust-162.txt"), screenFixture(t, "claude-2.1.312-empty-162.txt"))

	s, err := n.start(context.Background(), "~/proj", "", "fix the build", "", "", "plan", "high", "", startID)
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionID != startID || s.PaneID != "w1:p2" || !s.StartedByJarvis || s.Launcher != "claude" || s.SpaceKind != "herdr" ||
		s.PermissionMode != "plan" || s.Effort != "high" {
		t.Errorf("session %+v", s)
	}
	if st, _ := n.state.get(startID); st.Pane != "w1:p2" || st.Cwd != cwd || st.Launcher != "claude" {
		t.Errorf("state %+v", st)
	}
	if n.panes["w1:p2"] != startID {
		t.Errorf("pane binding %v", n.panes)
	}
	assertTrace(t, f.trace(), []string{
		call("workspace.list", nil),
		call("session.snapshot", nil),
		call("tab.create", map[string]any{"workspace_id": "w1", "cwd": cwd, "label": "agent-2", "focus": false}),
		call("pane.send_input", map[string]any{"pane_id": "w1:p2", "keys": []string{"Enter"},
			"text": startCmd(n, "claude", "--session-id "+startID+" --permission-mode plan --effort high")}),
		readVisible("w1:p2"), // the trust dialog, cursor on "No, exit"
		call("pane.send_keys", map[string]any{"pane_id": "w1:p2", "keys": []string{"down", "enter"}}),
		readVisible("w1:p2"), agentGet("w1:p2"), // ready once…
		readVisible("w1:p2"), agentGet("w1:p2"), // …and twice
		call("agent.rename", map[string]any{"target": "w1:p2", "name": "j-0e0000"}),
		call("agent.prompt", map[string]any{"target": "w1:p2", "text": "fix the build"}),
		call("session.snapshot", nil), // sessionFor
	})
}

// No workspace for the directory: one is made (labelled with the directory's name, not focused) and its first tab
// renamed; a resume types --resume and no first message.
func TestHerdrStartCreatesTheWorkspaceAndResumes(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := filepath.Join(home, "fresh")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	f.reply("workspace.list", map[string]any{"workspaces": []any{}})
	f.reply("workspace.create", map[string]any{"workspace": map[string]any{"workspace_id": "w7"},
		"tab": map[string]any{"tab_id": "w7:t1"}, "root_pane": map[string]any{"pane_id": "w7:p1"}})
	startHerdr(f, "w7:p1", proto.StatusDone)
	f.setScreen("w7:p1", screenFixture(t, "claude-2.1.312-empty-162.txt"))

	if _, err := n.start(context.Background(), cwd, "work_claude", "", startID, "", "", "", "  fix\nthe   build ", ""); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		call("workspace.list", nil),
		call("workspace.create", map[string]any{"cwd": cwd, "label": "fresh", "focus": false}),
		call("tab.rename", map[string]any{"tab_id": "w7:t1", "label": "fix the build"}),
		call("pane.send_input", map[string]any{"pane_id": "w7:p1", "keys": []string{"Enter"},
			"text": startCmd(n, "work_claude", "--resume "+startID)}),
		readVisible("w7:p1"), agentGet("w7:p1"),
		readVisible("w7:p1"), agentGet("w7:p1"),
		call("agent.rename", map[string]any{"target": "w7:p1", "name": "j-0e0000"}),
		call("session.snapshot", nil),
	})
}

// A workspace given (a tab next to another agent): no workspace lookup. A claude that comes up blocked (a dialog
// of its own) counts as ready at once.
func TestHerdrStartIntoAGivenWorkspaceReturnsOnABlockedClaude(t *testing.T) {
	n, f, home := herdrNode(t)
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w2:t5"}, "root_pane": map[string]any{"pane_id": "w2:p5"}})
	startHerdr(f, "w2:p5", proto.StatusBlocked)
	f.setScreen("w2:p5", screenFixture(t, "claude-2.1.312-model-picker-162.txt"))
	s, err := n.start(context.Background(), "", "claude", "hello", "", "w2", "", "", "a very long tab name that goes past thirty-two runes", startID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != proto.StatusBlocked {
		t.Errorf("status %s", s.Status)
	}
	assertTrace(t, f.trace(), []string{
		call("session.snapshot", nil),
		call("tab.create", map[string]any{"workspace_id": "w2", "cwd": home, "label": "a very long tab name that goes p", "focus": false}),
		call("pane.send_input", map[string]any{"pane_id": "w2:p5", "keys": []string{"Enter"},
			"text": startCmd(n, "claude", "--session-id "+startID)}),
		readVisible("w2:p5"), agentGet("w2:p5"),
		call("agent.rename", map[string]any{"target": "w2:p5", "name": "j-0e0000"}),
		call("agent.prompt", map[string]any{"target": "w2:p5", "text": "hello"}),
		call("session.snapshot", nil),
	})
}

// Resuming a session that is already live does nothing in herdr but refresh the layout; herdr not running fails
// before anything is created.
func TestHerdrStartOfALiveSessionAndWithoutHerdr(t *testing.T) {
	n, f, home := herdrNode(t)
	startHerdr(f, "w1:p1", proto.StatusIdle)
	n.agents[startID] = herdr.Agent{Agent: "claude", PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", AgentStatus: proto.StatusIdle}
	s, err := n.start(context.Background(), home, "", "", startID, "", "", "", "", "")
	if err != nil || s.PaneID != "w1:p1" {
		t.Fatalf("%+v %v", s, err)
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil)})

	f.reset()
	f.reply("workspace.list", herdrFail{Code: "internal", Message: "down"})
	if _, err := n.start(context.Background(), home, "", "", "", "", "", "", "", ""); err == nil ||
		!strings.Contains(err.Error(), "herdr is not running") {
		t.Fatalf("err %v", err)
	}
	assertTrace(t, f.trace(), []string{call("workspace.list", nil)})
}

// prompt: agent.prompt; an agent herdr does not detect (not_found) gets the text typed and enter pressed; other
// errors are returned as they are.
func TestHerdrPromptFallsBackToTyping(t *testing.T) {
	n, f, _ := herdrNode(t)
	ctx := context.Background()
	if err := n.prompt(ctx, "w1:p1", "hi"); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{call("agent.prompt", map[string]any{"target": "w1:p1", "text": "hi"})})

	f.reset()
	f.reply("agent.prompt", herdrFail{Code: "agent_not_found", Message: "no agent in pane"})
	if err := n.prompt(ctx, "w1:p2", "line one\nline two"); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		call("agent.prompt", map[string]any{"target": "w1:p2", "text": "line one\nline two"}),
		call("pane.send_text", map[string]any{"pane_id": "w1:p2", "text": "line one\nline two"}),
		call("pane.send_keys", map[string]any{"pane_id": "w1:p2", "keys": []string{"enter"}}),
	})

	f.reset()
	f.reply("agent.prompt", herdrFail{Code: "internal", Message: "boom"})
	if err := n.prompt(ctx, "w1:p3", "x"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
	assertTrace(t, f.trace(), []string{call("agent.prompt", map[string]any{"target": "w1:p3", "text": "x"})})
}

// session.keys: pre_keys first (their own send_keys), then the text, then the keys; session.interrupt is esc.
// A session herdr does not have is looked for in one snapshot and refused.
func TestHerdrKeysWithPreKeys(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	n.agents[sidA] = herdr.Agent{Agent: "claude", PaneID: "w1:p1"}
	ctx := context.Background()
	params, _ := json.Marshal(map[string]any{"session_id": sidA, "pre_keys": []string{"down", "down"}, "text": "other answer",
		"keys": []string{"enter"}})
	if _, err := n.handle(ctx, "session.keys", params); err != nil {
		t.Fatal(err)
	}
	params, _ = json.Marshal(map[string]any{"session_id": sidA})
	if _, err := n.handle(ctx, "session.interrupt", params); err != nil {
		t.Fatal(err)
	}
	if err := n.keys(ctx, sidA, nil, ""); err != nil { // nothing to send: no call
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		call("pane.send_keys", map[string]any{"pane_id": "w1:p1", "keys": []string{"down", "down"}}),
		call("pane.send_text", map[string]any{"pane_id": "w1:p1", "text": "other answer"}),
		call("pane.send_keys", map[string]any{"pane_id": "w1:p1", "keys": []string{"enter"}}),
		call("pane.send_keys", map[string]any{"pane_id": "w1:p1", "keys": []string{"esc"}}),
	})

	f.reset()
	if err := n.keys(ctx, sidB, []string{"enter"}, ""); err == nil {
		t.Fatal("keys to a session that is not running")
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil)})
}

// close: ctrl+c twice, then the pane alone in a split tab, else the whole tab. A session Jarvis started that is gone
// is already closed; one herdr never had cannot be closed.
func TestHerdrClose(t *testing.T) {
	n, f, _ := herdrNode(t)
	ctx := context.Background()
	n.agents[sidA] = herdr.Agent{Agent: "claude", PaneID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1"}
	n.agents[sidB] = herdr.Agent{Agent: "claude", PaneID: "w1:p2", TabID: "w1:t2", WorkspaceID: "w1"}
	f.reply("pane.list", map[string]any{"panes": []any{
		map[string]any{"tab_id": "w1:t1"}, map[string]any{"tab_id": "w1:t1"}, map[string]any{"tab_id": "w1:t2"}}})
	ctrlC := func(pane string) string {
		return call("pane.send_keys", map[string]any{"pane_id": pane, "keys": []string{"ctrl+c"}})
	}
	if err := n.close(ctx, sidA); err != nil {
		t.Fatal(err)
	}
	if err := n.close(ctx, sidB); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		ctrlC("w1:p1"), ctrlC("w1:p1"),
		call("pane.list", map[string]any{"workspace_id": "w1"}),
		call("pane.close", map[string]any{"pane_id": "w1:p1"}),
		ctrlC("w1:p2"), ctrlC("w1:p2"),
		call("pane.list", map[string]any{"workspace_id": "w1"}),
		call("tab.close", map[string]any{"tab_id": "w1:t2"}),
	})

	f.reset()
	n.state.put(sidC, sessionState{Launcher: "claude"})
	if err := n.close(ctx, sidC); err != nil {
		t.Fatalf("gone already: %v", err)
	}
	if err := n.close(ctx, "0d000000-0000-4000-8000-0000000000ff"); err == nil {
		t.Fatal("closed a session herdr never had")
	}
	assertTrace(t, f.trace(), nil)
}

// editQueue: up pulls the queue into the box, ctrl+u + backspace once per line clears it, a last ctrl+u, then the
// kept messages are prompted again in the order given.
func TestHerdrEditQueue(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := "/tmp/queue"
	writeTranscript(t, home, cwd, sidA, time.Now(),
		`{"type":"queue-operation","operation":"enqueue","content":"first"}`,
		`{"type":"queue-operation","operation":"enqueue","content":"second\nhas two lines"}`,
		`{"type":"queue-operation","operation":"enqueue","content":"third"}`)
	n.agents[sidA] = herdr.Agent{Agent: "claude", PaneID: "w1:p1", Cwd: cwd}
	queue, err := n.editQueue(context.Background(), sidA, []int{2, 0, 7})
	if err != nil || strings.Join(queue, "|") != "first|second\nhas two lines|third" {
		t.Fatalf("queue %q %v", queue, err)
	}
	keys := func(k ...string) string { return call("pane.send_keys", map[string]any{"pane_id": "w1:p1", "keys": k}) }
	assertTrace(t, f.trace(), []string{
		keys("up"),
		keys("ctrl+u", "backspace"), keys("ctrl+u", "backspace"), keys("ctrl+u", "backspace"), keys("ctrl+u", "backspace"),
		keys("ctrl+u"),
		call("agent.prompt", map[string]any{"target": "w1:p1", "text": "third"}),
		call("agent.prompt", map[string]any{"target": "w1:p1", "text": "first"}),
	})

	// Nothing queued: no keys.
	f.reset()
	writeTranscript(t, home, cwd, sidB, time.Now(), `{"type":"custom-title","customTitle":"x"}`)
	n.agents[sidB] = herdr.Agent{Agent: "claude", PaneID: "w1:p2", Cwd: cwd}
	if _, err := n.editQueue(context.Background(), sidB, nil); err == nil {
		t.Fatal("nothing queued")
	}
	assertTrace(t, f.trace(), nil)
}

// renameSpace renames the workspace; renameSession renames the tab of a live session and sends /rename for the title,
// and writes the title into the transcript of a session that is not running.
func TestHerdrRenames(t *testing.T) {
	n, f, home := herdrNode(t)
	ctx := context.Background()
	if err := n.renameSpace(ctx, "w1", "  backend "); err != nil {
		t.Fatal(err)
	}
	if err := n.renameSpace(ctx, "w1", " "); err == nil {
		t.Fatal("empty name")
	}
	n.agents[sidA] = herdr.Agent{Agent: "claude", PaneID: "w1:p1", TabID: "w1:t1"}
	tab, title := " review ", "  fix   the\nbuild "
	if err := n.renameSession(ctx, sidA, &tab, &title); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		call("workspace.rename", map[string]any{"workspace_id": "w1", "label": "backend"}),
		call("tab.rename", map[string]any{"tab_id": "w1:t1", "label": "review"}),
		call("agent.prompt", map[string]any{"target": "w1:p1", "text": "/rename fix the build"}),
	})

	// Not running: the tab cannot be renamed; the title goes into the transcript, no herdr call.
	f.reset()
	p := writeTranscript(t, home, "/tmp/r", sidB, time.Now(), `{"type":"custom-title","customTitle":"old"}`)
	if err := n.renameSession(ctx, sidB, &tab, nil); err == nil {
		t.Fatal("renamed the tab of a session that is not running")
	}
	newTitle := "new title"
	if err := n.renameSession(ctx, sidB, nil, &newTitle); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasSuffix(string(b), `{"customTitle":"new title","sessionId":"`+sidB+`","type":"custom-title"}`+"\n") {
		t.Errorf("transcript %s", b)
	}
	assertTrace(t, f.trace(), nil)
}
