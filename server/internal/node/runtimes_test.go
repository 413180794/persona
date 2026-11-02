package node

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// runtimeHub is the hub end of a node under test: the events it got and how it answers interactions.
type runtimeHub struct {
	mu       sync.Mutex
	events   []string
	lists    [][]proto.Session
	items    []json.RawMessage
	deltas   []json.RawMessage
	cancels  []string
	asked    []proto.Interaction
	resolve  func(proto.Interaction) proto.Resolution
	nodePeer *rpc.Peer
}

func (b *runtimeHub) lastList() []proto.Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lists) == 0 {
		return nil
	}
	return b.lists[len(b.lists)-1]
}

// runtimeNode is a node with a runtime socket in a short temp dir (unix socket paths are short on macOS) and a fake hub.
func runtimeNode(t *testing.T) (*Node, *runtimeHub) {
	t.Helper()
	return runtimeNodeOn(t, nil)
}

// runtimeNodeOn is runtimeNode serving a hub connection on a resident node's shared runtime host (nil: its own socket).
func runtimeNodeOn(t *testing.T, shared *RuntimeHost) (*Node, *runtimeHub) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "jb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	n, _ := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	n.runtimes = newRuntimeRegistry()
	n.runtimeSock = filepath.Join(dir, "h.sock")
	n.hosts = herdrHosts(&herdr.Client{Bin: filepath.Join(dir, "no-herdr")}) // herdr unreachable: no socket, no binary
	// No running claude to find: the real scan found the hub's assistant, and the list read its transcripts.
	n.cdirs = newClaudeDirs()
	n.cdirs.scan = func() map[int]procEnv { return nil }
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}

	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	n.peer.OnRequest = n.handle
	hub := rpc.New("h", h2nW)
	end := &runtimeHub{nodePeer: hub, resolve: func(proto.Interaction) proto.Resolution { return proto.Resolution{Dismiss: true} }}
	hub.OnEvent = func(event string, params json.RawMessage) {
		end.mu.Lock()
		defer end.mu.Unlock()
		end.events = append(end.events, event)
		switch event {
		case "sessions.changed":
			var p struct {
				Sessions []proto.Session `json:"sessions"`
			}
			_ = json.Unmarshal(params, &p)
			end.lists = append(end.lists, p.Sessions)
		case "session.items":
			end.items = append(end.items, params)
		case "session.delta":
			end.deltas = append(end.deltas, params)
		case "interaction.cancel":
			var p struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(params, &p)
			end.cancels = append(end.cancels, p.ID)
		}
	}
	hub.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "interaction.create":
			var p struct {
				Interaction proto.Interaction `json:"interaction"`
			}
			_ = json.Unmarshal(params, &p)
			end.mu.Lock()
			end.asked = append(end.asked, p.Interaction)
			resolve := end.resolve
			end.mu.Unlock()
			return resolve(p.Interaction), nil
		}
		return nil, errors.New("unexpected hub call " + method)
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	if shared != nil {
		n.runtimeSock = shared.path
		shared.attach(n)
	} else if _, err := n.listenRuntimeSocket(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return n, end
}

// fakeRuntime is a runtime: what it was asked, and how it answers.
type fakeRuntime struct {
	peer   *rpc.Peer
	conn   net.Conn
	mu     sync.Mutex
	calls  []string
	params map[string][]json.RawMessage
	answer func(method string, params json.RawMessage) (any, error)
	events []string       // event names the node pushed, in order
	hubs   []bool         // runtime.hub pushes: whether a hub is connected
	helloR map[string]any // what runtime.hello answered
}

func (f *fakeRuntime) got(method string) []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]json.RawMessage(nil), f.params[method]...)
}

func dialRuntime(t *testing.T, n *Node, hello map[string]any) *fakeRuntime {
	t.Helper()
	return dialRuntimeAt(t, n.runtimeSock, hello)
}

// dialRuntimeAt connects a fake runtime to the runtime socket at path and says hello.
func dialRuntimeAt(t *testing.T, path string, hello map[string]any) *fakeRuntime {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRuntime{conn: c, params: map[string][]json.RawMessage{}}
	f.peer = rpc.New("d", c)
	f.peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		f.mu.Lock()
		f.calls = append(f.calls, method)
		f.params[method] = append(f.params[method], params)
		answer := f.answer
		f.mu.Unlock()
		if answer != nil {
			return answer(method, params)
		}
		return map[string]any{"ok": true}, nil
	}
	f.peer.OnEvent = func(event string, params json.RawMessage) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.events = append(f.events, event)
		if event == "runtime.hub" {
			var p struct {
				Connected bool `json:"connected"`
			}
			_ = json.Unmarshal(params, &p)
			f.hubs = append(f.hubs, p.Connected)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = f.peer.Serve(ctx, c) }()
	t.Cleanup(func() { cancel(); _ = c.Close() })
	var res map[string]any
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()
	if err := f.peer.Call(cctx, "runtime.hello", hello, &res); err != nil {
		t.Fatal(err)
	}
	if p, _ := res["protocol"].(float64); int(p) != RuntimeProtocol {
		t.Fatalf("protocol %v", res["protocol"])
	}
	f.helloR = res
	return f
}

func dshHello() map[string]any {
	return map[string]any{"runtime": "dsh", "name": "DSH", "version": "test", "launcher": "dsh", "launcher_model": "DeepSeek",
		"caps": map[string]any{"start": true, "send": true, "interrupt": true, "rename": true}}
}

func nodeCall(t *testing.T, n *Node, method string, params any) (json.RawMessage, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := n.handle(ctx, method, raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	return b, nil
}

func listed(t *testing.T, n *Node) map[string]proto.Session {
	t.Helper()
	raw, err := nodeCall(t, n, "sessions.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Sessions []proto.Session `json:"sessions"`
	}
	_ = json.Unmarshal(raw, &res)
	out := map[string]proto.Session{}
	for _, s := range res.Sessions {
		out[s.SessionID] = s
	}
	return out
}

func TestRuntimeSessionsAreListedAndPushed(t *testing.T) {
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": "session-0a1b", "title": "fix the build", "cwd": "/w/app", "status": "working", "space_name": "app"},
		{"session_id": "../etc", "title": "not a valid id"},
	}})
	waitFor(t, "sessions.changed with the host's session", func() bool {
		for _, s := range hub.lastList() {
			if s.SessionID == "session-0a1b" {
				return true
			}
		}
		return false
	})
	s := listed(t, n)["session-0a1b"]
	if s.Agent != "dsh" || s.AgentLabel != "DSH" || s.SpaceKind != "dsh" || s.Status != proto.StatusWorking || s.Title != "fix the build" || s.SpaceName != "app" {
		t.Fatalf("listed as %+v", s)
	}
	// The source fields the apps gate controls on (as for Codex app-server sessions): what the runtime declared.
	c := s.Capabilities
	if s.SourceKind != proto.SourceRuntime || s.SourceInstanceID != "dsh" || c == nil || !c.Read || !c.Send || !c.Interrupt || !c.Approve ||
		c.Terminal || c.Attach || s.ReadOnlyReason != "" {
		t.Fatalf("source %q/%q caps %+v reason %q", s.SourceKind, s.SourceInstanceID, c, s.ReadOnlyReason)
	}
	if _, bad := listed(t, n)["../etc"]; bad {
		t.Fatal("an invalid id was listed")
	}
	if s.PaneID != "" {
		t.Fatal("a runtime session has no pane")
	}
}

// A runtime that hosts an agent the apps know (here Claude Code) says so: its sessions carry that agent and no label of
// the runtime's; a runtime without a name is labelled by its id; a hello without a runtime id is refused.
func TestRuntimeAgentAndLabel(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, map[string]any{"runtime": "acme-cloud", "agent": "claude"})
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": "c1", "status": "idle"},
		{"session_id": "c2", "agent": "acme-cloud", "status": "idle"},
	}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["c2"]; return ok })
	got := listed(t, n)
	if s := got["c1"]; s.Agent != "claude" || s.AgentLabel != "" || s.SpaceKind != "acme-cloud" {
		t.Fatalf("c1 %+v", s)
	}
	if s := got["c2"]; s.AgentLabel != "acme-cloud" {
		t.Fatalf("c2 %+v", s)
	}

	c, err := net.Dial("unix", n.runtimeSock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := rpc.New("x", c)
	go func() { _ = p.Serve(context.Background(), c) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Call(ctx, "runtime.hello", map[string]any{"name": "No id"}, nil); err == nil {
		t.Fatal("a hello without a runtime id was taken")
	}
}

func TestRuntimeLeavingTakesItsSessions(t *testing.T) {
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-1", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-1"]; return ok })
	_ = host.conn.Close()
	waitFor(t, "gone from the pushed list", func() bool {
		for _, s := range hub.lastList() {
			if s.SessionID == "session-1" {
				return false
			}
		}
		return len(hub.lists) > 1
	})
	if _, ok := listed(t, n)["session-1"]; ok {
		t.Fatal("still listed after its host left")
	}
}

func TestRuntimeForwardsSessionCalls(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.items" {
			var items []proto.Item
			for i := 1; i <= 4; i++ { // four tool calls with results: folded on a page counted back from the end
				items = append(items,
					proto.Item{ID: "u", Seq: i * 2, Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "c" + string(rune('0'+i)), ToolName: "bash"},
					proto.Item{ID: "r", Seq: i*2 + 1, Role: "user", Kind: proto.KindToolResult, ToolUseID: "c" + string(rune('0'+i)), Text: "ok"})
			}
			items = append([]proto.Item{{ID: "p", Seq: 1, Role: "user", Kind: proto.KindText, Text: "run it"}}, items...)
			return map[string]any{"items": items, "has_more": false, "history": "h1", "to_seq": 9}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-2", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-2"]; return ok })

	for i := 0; i < 2; i++ { // the same client_msg_id twice (an app retry): sent once
		if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": "session-2", "text": "hi", "client_msg_id": "m-1"}); err != nil {
			t.Fatal(err)
		}
	}
	if sent := host.got("session.send"); len(sent) != 1 {
		t.Fatalf("host got %d sends", len(sent))
	} else {
		var p struct {
			Text        string `json:"text"`
			ClientMsgID string `json:"client_msg_id"`
		}
		_ = json.Unmarshal(sent[0], &p)
		if p.Text != "hi" || p.ClientMsgID != "m-1" {
			t.Fatalf("sent %s", sent[0])
		}
	}
	if _, err := nodeCall(t, n, "session.interrupt", map[string]any{"session_id": "session-2"}); err != nil || len(host.got("session.interrupt")) != 1 {
		t.Fatalf("interrupt: %v", err)
	}

	raw, err := nodeCall(t, n, "session.items", map[string]any{"session_id": "session-2", "turns": 2, "fold_work": true, "lazy_images": true})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items   []proto.Item `json:"items"`
		History string       `json:"history"`
		ToSeq   int          `json:"to_seq"`
	}
	_ = json.Unmarshal(raw, &page)
	if page.History != "h1" || page.ToSeq != 9 || len(page.Items) != 2 || page.Items[1].Kind != proto.KindFold {
		t.Fatalf("page %s", raw)
	}
	raw, _ = nodeCall(t, n, "session.items", map[string]any{"session_id": "session-2", "after_seq": 3})
	_ = json.Unmarshal(raw, &page)
	if len(page.Items) != 9 {
		t.Fatalf("an after_seq page is not folded: %d items", len(page.Items))
	}

	_, err = nodeCall(t, n, "session.keys", map[string]any{"session_id": "session-2", "keys": []string{"esc"}})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "unsupported" {
		t.Fatalf("keys: %v", err)
	}
	if raw, err := nodeCall(t, n, "session.rename", map[string]any{"session_id": "session-2", "tab_name": "x"}); err != nil || len(host.got("session.rename")) != 0 {
		t.Fatalf("a tab name goes nowhere: %s %v", raw, err)
	}
	if _, err := nodeCall(t, n, "session.rename", map[string]any{"session_id": "session-2", "title": "New name"}); err != nil || len(host.got("session.rename")) != 1 {
		t.Fatalf("rename: %v", err)
	}
}

func TestRuntimeSendRefusedWithoutTheCap(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, map[string]any{"runtime": "dsh"})
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-3"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-3"]; return ok })
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": "session-3", "text": "hi"}); err == nil {
		t.Fatal("sent without the send cap")
	}
	if len(host.got("session.send")) != 0 {
		t.Fatal("the host was asked to send")
	}
	if s := listed(t, n)["session-3"]; s.Capabilities == nil || s.Capabilities.Send || s.ReadOnlyReason == "" {
		t.Fatalf("a runtime without send is listed read-only with a reason: %+v %q", s.Capabilities, s.ReadOnlyReason)
	}
}

func TestRuntimeItemsAndDeltasReachTheHub(t *testing.T) {
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-4"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-4"]; return ok })
	if _, err := nodeCall(t, n, "session.watch", map[string]any{"session_id": "session-4", "since_seq": 5}); err != nil {
		t.Fatal(err)
	}
	if w := host.got("session.watch"); len(w) != 1 {
		t.Fatal("watch not forwarded")
	}
	_ = host.peer.Notify("session.items", map[string]any{"session_id": "session-4", "from_seq": 5, "to_seq": 7, "history": "h",
		"items": []proto.Item{{ID: "a", Seq: 7, Role: "assistant", Kind: proto.KindText, Text: "done"}}})
	_ = host.peer.Notify("session.delta", map[string]any{"session_id": "session-4", "text": "do"})
	_ = host.peer.Notify("session.items", map[string]any{"session_id": "someone-else", "to_seq": 1})
	waitFor(t, "items and delta", func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.items) == 1 && len(hub.deltas) == 1
	})
	time.Sleep(50 * time.Millisecond)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.items) != 1 {
		t.Fatalf("items for a session the host does not list were forwarded: %d", len(hub.items))
	}
	if n.runtimes.watching["session-4"] != 7 {
		t.Fatalf("watched seq %d", n.runtimes.watching["session-4"])
	}
}

func TestRuntimeRewatchesWhenItComesBack(t *testing.T) {
	n, _ := runtimeNode(t)
	first := dialRuntime(t, n, dshHello())
	_ = first.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-5"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-5"]; return ok })
	if _, err := nodeCall(t, n, "session.watch", map[string]any{"session_id": "session-5", "since_seq": 12}); err != nil {
		t.Fatal(err)
	}
	_ = first.conn.Close()
	waitFor(t, "unlisted", func() bool { _, ok := listed(t, n)["session-5"]; return !ok })

	second := dialRuntime(t, n, dshHello())
	_ = second.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-5"}}})
	waitFor(t, "watched again", func() bool { return len(second.got("session.watch")) == 1 })
	var p struct {
		SinceSeq int `json:"since_seq"`
	}
	_ = json.Unmarshal(second.got("session.watch")[0], &p)
	if p.SinceSeq != 12 {
		t.Fatalf("rewatched since %d", p.SinceSeq)
	}
}

func TestRuntimeInteractionGoesToThePhone(t *testing.T) {
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-6", "title": "deploy", "cwd": "/w/app"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-6"]; return ok })
	allow := true
	hub.mu.Lock()
	hub.resolve = func(ix proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &allow} }
	hub.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var res proto.Resolution
	err := host.peer.Call(ctx, "interaction.create", map[string]any{"interaction": map[string]any{
		"kind": "permission", "session_id": "session-6", "tool_name": "bash", "tool_use_id": "call_1",
		"tool_input": map[string]any{"command": "rm -rf build"}, "summary": "rm -rf build"}}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if res.Allow == nil || !*res.Allow {
		t.Fatalf("resolution %+v", res)
	}
	hub.mu.Lock()
	ix := hub.asked[0]
	hub.mu.Unlock()
	if ix.ID == "" || ix.SessionTitle != "deploy" || ix.Cwd != "/w/app" || !ix.Resumable || ix.ExpiresAt.IsZero() {
		t.Fatalf("asked %+v", ix)
	}
	if ix.Agent != "dsh" || ix.AgentLabel != "DSH" { // drawn as DSH's card, not Claude's (whose 本会话总是允许 DSH cannot keep)
		t.Fatalf("agent %q label %q", ix.Agent, ix.AgentLabel)
	}

	// Asked again for the same tool call (through the next node): the same id, so the hub hangs it on the same card.
	_ = host.peer.Call(ctx, "interaction.create", map[string]any{"interaction": map[string]any{
		"kind": "permission", "session_id": "session-6", "tool_name": "bash", "tool_use_id": "call_1"}}, &res)
	hub.mu.Lock()
	again := hub.asked[1].ID
	hub.mu.Unlock()
	if again != ix.ID {
		t.Fatalf("ids %s / %s", ix.ID, again)
	}

	hub.mu.Lock()
	hub.resolve = func(proto.Interaction) proto.Resolution { return proto.Resolution{Dismiss: true} }
	hub.mu.Unlock()
	res = proto.Resolution{}
	_ = host.peer.Call(ctx, "interaction.create", map[string]any{"interaction": map[string]any{
		"kind": "question", "session_id": "session-6", "questions": []map[string]any{{"question": "Which?", "options": []map[string]any{{"label": "A"}}}}}}, &res)
	if !res.Dismiss {
		t.Fatalf("a dismissed card answers dismiss: %+v", res)
	}
	if err := host.peer.Call(ctx, "interaction.create", map[string]any{"interaction": map[string]any{
		"kind": "permission", "session_id": "not-listed"}}, &res); err == nil {
		t.Fatal("asked for a session the host does not list")
	}
}

func TestRuntimeStartFromTheLauncher(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "session-new", "cwd": "/w/app", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	launcherCache.Lock()
	launcherCache.at, launcherCache.list = time.Now(), []proto.Launcher{{Name: "claude"}}
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = time.Time{}, nil
		launcherCache.Unlock()
	})
	raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var ls struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	_ = json.Unmarshal(raw, &ls)
	if len(ls.Launchers) != 2 || ls.Launchers[1].Name != "dsh" || ls.Launchers[1].Source != "dsh" || ls.Launchers[1].Agent != "dsh" {
		t.Fatalf("launchers %s", raw)
	}
	raw, err = nodeCall(t, n, "session.start", map[string]any{"cwd": "/w/app", "launcher": "dsh", "async": true})
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		Session proto.Session `json:"session"`
	}
	_ = json.Unmarshal(raw, &started)
	if started.Session.SessionID != "session-new" || started.Session.Agent != "dsh" {
		t.Fatalf("started %s", raw)
	}
	if _, ok := listed(t, n)["session-new"]; !ok {
		t.Fatal("not listed right after the start")
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": "session-new", "text": "go"}); err != nil {
		t.Fatal(err)
	}
}

func TestPollAgentsPublishesWithoutHerdr(t *testing.T) {
	n, hub := runtimeNode(t)
	n.events = newHerdrEvents()
	n.metas = map[string]quickMeta{}
	n.bgScans = map[string]*bgScan{}
	n.panePIDs = map[string]panePID{}
	n.modes = map[string]sessionModes{}
	n.driving = map[string]bool{}
	n.panes = map[string]string{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.pollAgents(ctx)
	// The host's list changes without a runtime.sessions push reaching publish directly: the poll has to carry it.
	h := n.runtimes.add(nil)
	h.mu.Lock()
	h.ready, h.hello.Runtime = true, "dsh"
	h.mu.Unlock()
	n.runtimes.setSessions(h, []proto.Session{{SessionID: "session-poll", Status: proto.StatusIdle, Agent: "dsh"}})
	waitFor(t, "a list pushed by the poll with herdr missing", func() bool {
		for _, s := range hub.lastList() {
			if s.SessionID == "session-poll" {
				return true
			}
		}
		return false
	})
}
