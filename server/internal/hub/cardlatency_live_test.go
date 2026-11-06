//go:build herdrlive

package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/node"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/screen"
)

// How long a question takes from a real Claude Code's terminal to an app (#751): a real node (in this process, with
// its own HOME so it never touches the machine's node) on an isolated herdr, wired to this hub as over SSH, and an app
// connection. Each round asks Claude for an AskUserQuestion and times every hop; odd rounds answer on the card, even
// ones on the terminal (the card has to go away).
//
//	HERDR_SOCKET_PATH=<the isolated herdr's socket> PERSONA_LIVE_CARD_PANE=<pane running claude, no Persona hooks>
//	[PERSONA_LIVE_CARD_ROUNDS=3] go test -tags herdrlive -run TestLiveCardLatency ./internal/hub/
func TestLiveCardLatency(t *testing.T) {
	pane := os.Getenv("PERSONA_LIVE_CARD_PANE")
	socket := os.Getenv("HERDR_SOCKET_PATH")
	if pane == "" || socket == "" {
		t.Skip("HERDR_SOCKET_PATH / PERSONA_LIVE_CARD_PANE not set")
	}
	rounds := 3
	if r, err := strconv.Atoi(os.Getenv("PERSONA_LIVE_CARD_ROUNDS")); err == nil && r > 0 {
		rounds = r
	}
	realHome, _ := os.UserHomeDir()
	home := t.TempDir()
	if err := os.Symlink(filepath.Join(realHome, ".claude"), filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	n, err := node.New("live")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var marks []mark
	note := func(what, detail string) {
		mu.Lock()
		marks = append(marks, mark{time.Now(), what, detail})
		mu.Unlock()
	}

	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "lab", Name: "lab"})
	h.byID["lab"] = m
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	peer := rpc.New("h", h2nW)
	peer.OnEvent = func(event string, params json.RawMessage) {
		if event == "sessions.changed" {
			var p struct {
				Sessions []proto.Session `json:"sessions"`
			}
			_ = json.Unmarshal(params, &p)
			for _, s := range p.Sessions {
				if s.PaneID == pane {
					note("node:sessions", s.Status)
				}
			}
		}
		m.onEvent(event, params)
	}
	peer.OnRequest = m.onRequest
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	go func() { _ = peer.Serve(ctx, n2hR) }()
	go func() { _ = n.Serve(ctx, h2nR, n2hW) }()
	m.mu.Lock()
	m.peer, m.online = peer, true
	m.mu.Unlock()

	app := &client{out: make(chan []byte, 8192), subs: map[[2]string]bool{}}
	h.mu.Lock()
	h.clients[app] = struct{}{}
	h.mu.Unlock()
	session := func() (proto.Session, bool) {
		for _, s := range m.sessionList() {
			if s.PaneID == pane {
				return s, true
			}
		}
		return proto.Session{}, false
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-app.out:
				var e struct {
					Type        string            `json:"type"`
					Interaction proto.Interaction `json:"interaction"`
					ID          string            `json:"id"`
				}
				_ = json.Unmarshal(b, &e)
				switch e.Type {
				case "interaction":
					if s, ok := session(); ok && e.Interaction.SessionID == s.SessionID {
						note("app:card", e.Interaction.ID)
					}
				case "interaction_resolved":
					note("app:gone", e.ID)
				}
			}
		}
	}()

	hc := &herdr.Client{Bin: "herdr", Socket: socket}
	go func() {
		_ = hc.Subscribe(ctx, []herdr.Subscription{{"type": "pane.agent_status_changed", "pane_id": pane}}, func(e herdr.Event) {
			var d struct {
				AgentStatus string `json:"agent_status"`
			}
			_ = json.Unmarshal(e.Data, &d)
			note("herdr:status", d.AgentStatus)
		})
	}()
	// The screen, read every 25 ms: when the dialog shows and when it is gone.
	go func() {
		was := false
		for ctx.Err() == nil {
			text, err := hc.PaneRead(ctx, pane, "visible", 50)
			if err == nil {
				is := screen.ParseDialog(text) != nil
				if is != was {
					note(map[bool]string{true: "screen:dialog", false: "screen:clear"}[is], "")
					was = is
				}
			}
			time.Sleep(25 * time.Millisecond)
		}
	}()

	waitFor := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("the node to list the pane", 30*time.Second, func() bool { _, ok := session(); return ok })

	var lines []string
	for i := 1; i <= rounds; i++ {
		waitFor("claude to be idle", 2*time.Minute, func() bool {
			s, _ := session()
			return s.Status != proto.StatusWorking && s.Status != proto.StatusBlocked
		})
		time.Sleep(2 * time.Second)
		mu.Lock()
		marks = nil
		mu.Unlock()
		prompt := fmt.Sprintf("Round %d: call the AskUserQuestion tool once, asking me which of two colours I prefer, with exactly "+
			"the options Red and Blue. Do not do anything else and do not use any other tool. After my answer reply with one word.", i)
		two := os.Getenv("PERSONA_LIVE_CARD_TWO") != ""
		if two {
			prompt = fmt.Sprintf("Round %d: call the AskUserQuestion tool once with two questions in that one call: which of two "+
				"colours I prefer (options Red and Blue), and which of two fruits (options Apple and Pear). Do not do anything "+
				"else and do not use any other tool. After my answers reply with one word.", i)
		}
		if err := hc.PaneSendText(ctx, pane, prompt); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		_ = hc.PaneSendKeys(ctx, pane, "Enter")
		note("sent", "")
		var ix proto.Interaction
		waitFor("the card", 3*time.Minute, func() bool {
			s, ok := session()
			if !ok {
				return false
			}
			for _, p := range h.pendingInteractions() {
				if p.SessionID == s.SessionID && p.Kind == proto.IxScreen && p.Dialog != nil {
					ix = p
					return true
				}
			}
			return false
		})
		note("hub:created", ix.CreatedAt.Format(time.RFC3339Nano))
		time.Sleep(time.Second)
		if two { // each question on its card; the next must get its own card while claude stays blocked
			for step := 1; ; step++ {
				var key string
				for _, o := range ix.Dialog.Options {
					if o.Kind == "" {
						key = o.Key
						break
					}
				}
				at := time.Now()
				if err := h.resolve(ix.ID, proto.Resolution{Option: key}); err != nil {
					t.Fatalf("answer %q: %v", ix.Dialog.Question, err)
				}
				t.Logf("round %d step %d: answered %q (%s) with %s", i, step, ix.Dialog.Question, ix.ID, key)
				var next proto.Interaction
				deadline := time.Now().Add(5 * time.Second)
				for next.ID == "" && time.Now().Before(deadline) {
					for _, p := range h.pendingInteractions() {
						if p.Kind == proto.IxScreen && p.Dialog != nil && p.ID != ix.ID {
							next = p
						}
					}
					time.Sleep(20 * time.Millisecond)
				}
				if next.ID == "" {
					t.Logf("round %d step %d: no further card; claude %s", i, step, func() string { s, _ := session(); return s.Status }())
					break
				}
				t.Logf("round %d step %d: next card %q %d ms after the answer", i, step, next.Dialog.Question, next.CreatedAt.Sub(at).Milliseconds())
				ix = next
				time.Sleep(time.Second)
			}
			continue
		}
		answered := time.Now()
		via := "card"
		if i%2 == 1 {
			var key string
			for _, o := range ix.Dialog.Options {
				if o.Kind == "" {
					key = o.Key
					break
				}
			}
			note("answer:card", key)
			if err := h.resolve(ix.ID, proto.Resolution{Option: key}); err != nil {
				t.Fatalf("answer on the card: %v", err)
			}
		} else {
			via = "terminal"
			note("answer:terminal", "")
			_ = hc.PaneSendKeys(ctx, pane, "Enter")
		}
		waitFor("the card to go", time.Minute, func() bool {
			_, still := h.pendingInteraction(ix.ID)
			return !still
		})
		time.Sleep(500 * time.Millisecond)
		mu.Lock()
		got := append([]mark(nil), marks...)
		mu.Unlock()
		lines = append(lines, summarize(i, via, got, ix.CreatedAt, answered))
		if os.Getenv("PERSONA_LIVE_CARD_TRACE") != "" {
			lines = append(lines, trace(got))
		}
	}
	for _, l := range lines {
		t.Log(l)
	}
}

type mark struct {
	at          time.Time
	what, extra string
}

// summarize is one round's hops, in ms from the dialog first seen on the terminal (and from the answer).
func summarize(round int, via string, ms []mark, created, answered time.Time) string {
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].at.Before(ms[j].at) })
	first := func(what, extra string, after time.Time) time.Time {
		for _, m := range ms {
			if m.what == what && (extra == "" || m.extra == extra) && !m.at.Before(after) {
				return m.at
			}
		}
		return time.Time{}
	}
	sent := first("sent", "", time.Time{})
	dialog := first("screen:dialog", "", sent)
	herdrBlocked := first("herdr:status", "blocked", sent)
	nodeBlocked := first("node:sessions", proto.StatusBlocked, sent)
	card := first("app:card", "", sent)
	d := func(a, b time.Time) string {
		if a.IsZero() || b.IsZero() {
			return "?"
		}
		return strconv.FormatInt(b.Sub(a).Milliseconds(), 10)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "round %d: dialog on screen → herdr blocked %s ms → hub has blocked %s ms → card made %s ms → app %s ms; total %s ms",
		round, d(dialog, herdrBlocked), d(herdrBlocked, nodeBlocked), d(nodeBlocked, created), d(created, card), d(dialog, card))
	gone := first("app:gone", "", answered)
	clear := first("screen:clear", "", answered)
	herdrMoved := time.Time{}
	for _, m := range ms {
		if m.what == "herdr:status" && m.extra != "blocked" && !m.at.Before(answered) {
			herdrMoved = m.at
			break
		}
	}
	nodeMoved := time.Time{}
	for _, m := range ms {
		if m.what == "node:sessions" && m.extra != proto.StatusBlocked && !m.at.Before(answered) {
			nodeMoved = m.at
			break
		}
	}
	fmt.Fprintf(&b, " | answered on the %s: screen clear %s ms, herdr %s ms, hub has it %s ms, card gone %s ms",
		via, d(answered, clear), d(answered, herdrMoved), d(answered, nodeMoved), d(answered, gone))
	return b.String()
}

// trace is the round's marks, in ms from the first.
func trace(ms []mark) string {
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].at.Before(ms[j].at) })
	var b strings.Builder
	for _, m := range ms {
		fmt.Fprintf(&b, " %s=%s@%d", m.what, m.extra, m.at.Sub(ms[0].at).Milliseconds())
	}
	return "  trace:" + b.String()
}
