package node

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

const ocSID = "ses_e5612a2ffeBQKMrUbj51TjJc"

// ocPaneAgent is an opencode in a herdr pane as herdr lists it: herdr's opencode plugin reports the session id and
// puts "OC | " before the title.
func ocPaneAgent(pane, cwd, session, status string) map[string]any {
	a := map[string]any{"agent": "opencode", "agent_status": status, "pane_id": pane, "tab_id": "w1:t3", "workspace_id": "w1",
		"cwd": cwd, "terminal_title_stripped": "OC | Fix the pars…", "terminal_title": "OC | Fix the pars…"}
	if session != "" {
		a["agent_session"] = map[string]any{"agent": "opencode", "kind": "id", "source": "herdr:opencode", "value": session}
	}
	return a
}

// ocExtension is the Persona plugin in an opencode server process: a runtime listing the sessions it serves.
func ocExtension(t *testing.T, rh *RuntimeHost, runtime string, rows ...map[string]any) *fakeRuntime {
	t.Helper()
	rt := dialRuntimeAt(t, rh.path, map[string]any{"runtime": runtime, "agent": "opencode", "name": "opencode", "caps": map[string]any{"send": true, "interrupt": true}})
	_ = rt.peer.Notify("runtime.sessions", map[string]any{"sessions": rows})
	waitFor(t, "the plugin's list", func() bool {
		for _, r := range rows {
			if rh.reg.ownerOf(r["session_id"].(string)) == nil {
				return false
			}
		}
		return true
	})
	return rt
}

func TestOpencodeTitle(t *testing.T) {
	for in, want := range map[string]string{
		"OC | Fix the parser": "Fix the parser",
		"oc | lower":          "lower",
		"OC |   spaced  ":     "spaced",
		"OC | ":               "",
		"OpenCode":            "",
		"OC":                  "",
		"No prefix":           "No prefix",
		"":                    "",
	} {
		if got := opencodeTitle(in); got != want {
			t.Errorf("opencodeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// An opencode in a herdr pane is one session: herdr's row (pane, space, tab, state, the terminal), completed by what the
// plugin in the server lists. The conversation and the control go to the plugin; the screen and the keys to herdr.
func TestOpencodeInHerdrIsOneHerdrSession(t *testing.T) {
	n, f, _, rh := piHerdrNode(t)
	rt := ocExtension(t, rh, "opencode-4242", map[string]any{"session_id": ocSID, "status": "working", "title": "Fix the parser for good",
		"last_text": "Reading the grammar.", "model": "model-x", "rev": 7, "cwd": "/w", "context_tokens": 4000, "context_window": 200000})
	seedAgents(t, n, f, ocPaneAgent("w1:p3", "/w", ocSID, proto.StatusIdle))

	s := onlyRow(t, listSessions(t, n), ocSID)
	if s.Agent != "opencode" || s.PaneID != "w1:p3" || s.SpaceKind != "herdr" || s.SourceKind != "" {
		t.Fatalf("not herdr's row: %+v", s)
	}
	if s.Title != "Fix the parser for good" || s.LastText != "Reading the grammar." || s.Model != "model-x" || s.Rev != 7 || s.ReadOnlyReason != "" {
		t.Fatalf("not completed by the plugin: %+v", s)
	}
	if s.ContextTokens != 4000 || s.ContextWindow != 200000 {
		t.Fatalf("context use: %+v", s)
	}
	if s.Status != proto.StatusIdle {
		t.Fatalf("the state is herdr's: %+v", s)
	}

	f.reset()
	if _, err := request(t, n, "session.send", map[string]any{"session_id": ocSID, "text": "go on", "client_msg_id": "m-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := request(t, n, "session.items", map[string]any{"session_id": ocSID, "turns": 2}); err != nil {
		t.Fatal(err)
	}
	if len(rt.got("session.send")) != 1 || len(rt.got("session.items")) != 1 {
		t.Fatalf("send %d, items %d at the plugin", len(rt.got("session.send")), len(rt.got("session.items")))
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "agent.prompt") || strings.Contains(c, "pane.send_text") {
			t.Fatalf("typed into the pane: %v", f.trace())
		}
	}
	f.setScreen("w1:p3", "OpenCode\n> ")
	if _, err := request(t, n, "session.screen", map[string]any{"session_id": ocSID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.trace(), "\n"), "pane.read") || len(rt.got("session.screen")) != 0 {
		t.Fatalf("the screen did not come from herdr: %v", f.trace())
	}
}

// herdr's state is the row's; the plugin adds "needs you" when a card waits on the phone (a permission the server asked).
func TestOpencodeBlockedFromThePlugin(t *testing.T) {
	n, f, _, rh := piHerdrNode(t)
	ocExtension(t, rh, "opencode-4243", map[string]any{"session_id": ocSID, "status": "blocked", "cwd": "/w"})
	seedAgents(t, n, f, ocPaneAgent("w1:p3", "/w", ocSID, proto.StatusWorking))
	if s := onlyRow(t, listSessions(t, n), ocSID); s.Status != proto.StatusBlocked {
		t.Fatalf("not needs-you: %+v", s)
	}
}

// herdr's own state: blocked (a permission asked in the TUI), working, done, all come from its report.
func TestOpencodeStateIsHerdrs(t *testing.T) {
	n, f, _, _ := piHerdrNode(t)
	for _, st := range []string{proto.StatusBlocked, proto.StatusWorking, proto.StatusDone, proto.StatusIdle} {
		seedAgents(t, n, f, ocPaneAgent("w1:p3", "/w", ocSID, st))
		if s := onlyRow(t, listSessions(t, n), ocSID); s.Status != st {
			t.Fatalf("herdr says %s: %+v", st, s)
		}
	}
}

// Without herdr's session report the plugin says which pane it runs in (when its server knows).
func TestOpencodeFoundByThePluginsPane(t *testing.T) {
	n, f, _, rh := piHerdrNode(t)
	ocExtension(t, rh, "opencode-4244", map[string]any{"session_id": ocSID, "status": "idle", "pane_id": "w1:p4", "cwd": "/w"})
	seedAgents(t, n, f, ocPaneAgent("w1:p4", "/w", "", proto.StatusIdle))
	if s := onlyRow(t, listSessions(t, n), ocSID); s.PaneID != "w1:p4" || s.Agent != "opencode" {
		t.Fatalf("%+v", s)
	}
}

// An opencode no plugin serves: listed with the title from the terminal (without herdr's prefix), what is missing, an
// empty conversation saying so, no sending (nothing typed into the TUI blind), Esc to interrupt.
func TestOpencodeWithoutThePlugin(t *testing.T) {
	n, f, _, _ := piHerdrNode(t)
	seedAgents(t, n, f, ocPaneAgent("w1:p3", "/w", ocSID, proto.StatusBlocked))
	s := onlyRow(t, listSessions(t, n), ocSID)
	if s.ReadOnlyReason != opencodeNoPlugin || s.PaneID != "w1:p3" || s.Title != "Fix the pars…" || s.Status != proto.StatusBlocked || s.Agent != "opencode" {
		t.Fatalf("%+v", s)
	}
	res, err := request(t, n, "session.items", map[string]any{"session_id": ocSID, "turns": 2})
	if err != nil || !strings.Contains(string(mustJSON(res)), "Persona 的插件") {
		t.Fatalf("items %v %v", res, err)
	}
	f.reset()
	_, err = request(t, n, "session.send", map[string]any{"session_id": ocSID, "text": "hi", "client_msg_id": "m-2"})
	if !isRefusal(err, "opencode_no_extension") {
		t.Fatalf("send: %v", err)
	}
	if _, err := request(t, n, "session.interrupt", map[string]any{"session_id": ocSID}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.trace(), "\n"); strings.Contains(got, "agent.prompt") || !strings.Contains(got, "escape") {
		t.Fatalf("herdr calls: %v", f.trace())
	}
	// The terminal is herdr's as for any pane.
	f.setScreen("w1:p3", "OpenCode\n> ")
	if _, err := request(t, n, "session.screen", map[string]any{"session_id": ocSID}); err != nil {
		t.Fatal(err)
	}
}

// An agent herdr reports that Persona cannot read is no longer dropped: a terminal-only row with its pane, state and
// title, an empty conversation saying so, nothing sent, the keys and the screen herdr's. Panes of no agent stay out.
func TestUnknownAgentIsListedTerminalOnly(t *testing.T) {
	n, f, _, _ := piHerdrNode(t)
	gem := map[string]any{"agent": "gemini", "agent_status": proto.StatusWorking, "pane_id": "w1:p5", "tab_id": "w1:t4", "workspace_id": "w1",
		"cwd": "/w", "terminal_title_stripped": "Gemini CLI", "terminal_title": "Gemini CLI"}
	none := map[string]any{"agent": "", "agent_status": "unknown", "pane_id": "w1:p6", "tab_id": "w1:t5", "workspace_id": "w1", "cwd": "/w"}
	seedAgents(t, n, f, gem, none)
	var rows []proto.Session
	for _, s := range listSessions(t, n) {
		if s.PaneID == "w1:p5" || s.PaneID == "w1:p6" {
			rows = append(rows, s)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	s := rows[0]
	if s.Agent != "gemini" || s.Status != proto.StatusWorking || s.Title != "Gemini CLI" || s.SessionID != "gemini-w1-p5" || s.ReadOnlyReason != otherNoReader("gemini") {
		t.Fatalf("%+v", s)
	}
	_, err := request(t, n, "session.send", map[string]any{"session_id": s.SessionID, "text": "hi", "client_msg_id": "m-3"})
	if !isRefusal(err, "terminal_only") {
		t.Fatalf("send: %v", err)
	}
	if _, err := request(t, n, "session.items", map[string]any{"session_id": s.SessionID, "turns": 2}); err != nil {
		t.Fatal(err)
	}
	f.setScreen("w1:p5", "gemini> ")
	f.reset()
	if _, err := request(t, n, "session.screen", map[string]any{"session_id": s.SessionID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.trace(), "\n"), "pane.read") {
		t.Fatalf("the screen did not come from herdr: %v", f.trace())
	}
	if _, err := request(t, n, "session.retire", map[string]any{"session_id": s.SessionID}); err == nil {
		t.Fatal("a pane of an agent nobody resumes was closed")
	}
}

func TestOtherSessionID(t *testing.T) {
	for _, c := range []struct{ agent, pane, want string }{
		{"gemini", "w1:p5", "gemini-w1-p5"},
		{"Amp", "wC:p1", "amp-wc-p1"},
		{"x/y", "w1:p2", "x-y-w1-p2"},
	} {
		if got := otherSession(host.Agent{Agent: c.agent, PaneID: c.pane}); got != c.want {
			t.Errorf("otherSession(%s, %s) = %q, want %q", c.agent, c.pane, got, c.want)
		}
	}
}

// herdr's opencode integration may report no session id (an older one): the pane is still listed, terminal only,
// instead of vanishing.
func TestOpencodePaneWithoutASessionID(t *testing.T) {
	n, f, _, _ := piHerdrNode(t)
	seedAgents(t, n, f, ocPaneAgent("w1:p7", "/w", "", proto.StatusWorking))
	s := onlyRow(t, listSessions(t, n), "opencode-w1-p7")
	if s.Agent != "opencode" || s.PaneID != "w1:p7" || s.Status != proto.StatusWorking || s.ReadOnlyReason != opencodeNoPlugin {
		t.Fatalf("%+v", s)
	}
	if _, err := request(t, n, "session.send", map[string]any{"session_id": s.SessionID, "text": "hi", "client_msg_id": "m-9"}); !isRefusal(err, "opencode_no_extension") {
		t.Fatalf("send: %v", err)
	}
}
