package node

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// shellPane is a herdr pane entry with no agent in it, marked as Persona's for session (none when "").
func shellPane(pane, tab, ws, cwd, session string) map[string]any {
	p := map[string]any{"pane_id": pane, "tab_id": tab, "workspace_id": ws, "cwd": cwd}
	if session != "" {
		p["tokens"] = map[string]any{personaTabToken: session}
	}
	return p
}

// A tab Persona opened for an agent that has exited is closed once only the shell has been in front, its screen
// unchanged, for shellTabIdle: the whole tab when it is its only pane, just the pane in a split tab. Tabs Persona did
// not open, ones where another program runs, and ones typed into since are left alone; the session keeps its state.
func TestShellTabSweepClosesPersonaTabsLeftWithAShell(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd, Model: "opus"})
	n.state.put(sidB, sessionState{Launcher: "cc", Cwd: cwd})
	n.state.put(sidC, sessionState{Launcher: "cc", Cwd: cwd})
	n.state.put(sidD, sessionState{Launcher: "cc", Cwd: cwd})
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent(sidD, proto.StatusIdle, "w1:p6", "w1:t6", "w1", cwd, ""),
	}, []any{
		shellPane("w1:p1", "w1:t1", "w1", cwd, sidA), // exited: closed with its tab
		shellPane("w1:p2", "w1:t2", "w1", cwd, ""),   // the user's own tab
		shellPane("w1:p3", "w1:t3", "w1", cwd, sidB), // something else runs in front
		shellPane("w1:p4", "w1:t4", "w1", cwd, sidC), // split: only this pane goes
		shellPane("w1:p5", "w1:t4", "w1", cwd, ""),
		shellPane("w1:p7", "w1:t7", "w1", cwd, "agent"), // typed into between the looks
		claudeAgent(sidD, proto.StatusIdle, "w1:p6", "w1:t6", "w1", cwd, ""), // a running agent's pane
	}))
	f.handle("pane.process_info", func(p map[string]any) any {
		if p["pane_id"] == "w1:p3" {
			return foreground([2]string{"zsh", "-zsh"}, [2]string{"make", "make"})
		}
		return foreground([2]string{"zsh", "-zsh"})
	})
	f.handle("pane.list", func(map[string]any) any {
		return map[string]any{"panes": []any{map[string]any{"tab_id": "w1:t4"}, map[string]any{"tab_id": "w1:t4"}}}
	})
	for _, p := range []string{"w1:p1", "w1:p2", "w1:p3", "w1:p4", "w1:p7"} {
		f.setScreen(p, "Resume this session with:\nclaude --resume "+p+"\n$ ")
	}
	listSessions(t, n)
	f.reset()

	t0 := time.Now()
	if got := n.sweepShellTabs(t.Context(), t0); got != 0 {
		t.Fatalf("closed %d at the first look", got)
	}
	f.setScreen("w1:p7", "$ ls\nREADME.md\n$ ")
	if got := n.sweepShellTabs(t.Context(), t0.Add(shellTabIdle-time.Second)); got != 0 {
		t.Fatalf("closed %d before shellTabIdle", got)
	}
	if got := n.sweepShellTabs(t.Context(), t0.Add(shellTabIdle+time.Second)); got != 2 {
		t.Fatalf("closed %d, want 2", got)
	}
	closes := f.trace("tab.close", "pane.close")
	want := []string{
		call("tab.close", map[string]any{"tab_id": "w1:t1"}),
		call("pane.close", map[string]any{"pane_id": "w1:p4"}),
	}
	slices.Sort(closes)
	slices.Sort(want)
	if !slices.Equal(closes, want) {
		t.Errorf("closed\n  %s\nwant\n  %s", strings.Join(closes, "\n  "), strings.Join(want, "\n  "))
	}
	for _, c := range f.trace("pane.process_info", "pane.read") {
		if strings.Contains(c, "w1:p2") || strings.Contains(c, "w1:p6") {
			t.Errorf("looked into a pane that is not an exited Persona tab: %s", c)
		}
	}
	if st, _ := n.state.get(sidA); st.Launcher != "cc" || st.Cwd != cwd || st.Model != "opus" {
		t.Errorf("state after the sweep = %+v", st)
	}

	// p7 was typed into: it waits shellTabIdle from then.
	f.reset()
	if got := n.sweepShellTabs(t.Context(), t0.Add(2*shellTabIdle+2*time.Second)); got != 1 {
		t.Fatalf("closed %d after p7's own idle time, want 1", got)
	}
	if got := f.trace("tab.close", "pane.close"); len(got) != 1 || got[0] != call("tab.close", map[string]any{"tab_id": "w1:t7"}) {
		t.Errorf("closed %q", got)
	}
}

// A tab whose session is being started or resumed right now, or that a close fails for, stays (and is not counted).
func TestShellTabSweepSkipsStartsAndReportsFailures(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd})
	n.state.put(sidB, sessionState{Launcher: "cc", Cwd: cwd})
	f.reply("session.snapshot", snapshot(nil, nil, nil, []any{
		shellPane("w1:p1", "w1:t1", "w1", cwd, sidA),
		shellPane("w1:p2", "w1:t2", "w1", cwd, sidB),
	}))
	f.reply("pane.process_info", foreground([2]string{"zsh", "-zsh"}))
	f.reply("tab.close", herdrFail{Code: "tab_not_found", Message: "no such tab"})
	listSessions(t, n)
	n.starts.add(sidA, &startRec{done: make(chan struct{})})
	t0 := time.Now()
	n.sweepShellTabs(t.Context(), t0)
	if got := n.sweepShellTabs(t.Context(), t0.Add(shellTabIdle+time.Second)); got != 0 {
		t.Fatalf("counted %d closes", got)
	}
	got := f.trace("tab.close")
	if len(got) != 1 || !strings.Contains(got[0], "w1:t2") {
		t.Errorf("tab.close calls %q, want only w1:t2's (sidA is starting)", got)
	}
}

// /exit (or /quit) sent to a running agent closes it as session.close does, tab and all, instead of typing it in and
// leaving the shell; sent to an agent that is not running it does nothing (no tab is opened to resume it).
func TestHerdrSendExitClosesTheAgent(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	writeTranscript(t, home, cwd, sidA, time.Now().Add(-time.Hour), `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"hi"},"uuid":"u1"}`)
	writeTranscript(t, home, cwd, sidB, time.Now().Add(-time.Hour), `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"hi"},"uuid":"u1"}`)
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd})
	n.state.put(sidB, sessionState{Launcher: "cc", Cwd: cwd})
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, ""),
	}, nil))
	listSessions(t, n)
	waitCalls(t, f, "pane.report_agent_session", 1)
	f.reset()

	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": " /exit\n"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, withoutAgentReports(f.trace("pane.send_keys", "pane.send_input", "agent.prompt", "pane.send_text", "pane.list", "tab.close")), []string{
		sendKeys("w1:p1", "ctrl+c"), sendKeys("w1:p1", "ctrl+c"),
		call("pane.list", map[string]any{"workspace_id": "w1"}),
		call("tab.close", map[string]any{"tab_id": "w1:t1"}),
	})

	f.reset()
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidB, "text": "/quit"}); err != nil {
		t.Fatal(err)
	}
	if got := f.trace("tab.create", "workspace.create", "pane.send_input", "agent.prompt"); len(got) != 0 {
		t.Errorf("an agent that is not running was resumed to quit: %q", got)
	}
}
