package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/connector/imessage"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// imessageNode is a Mac's node over the real RPC pipe, answering connector.* from answer (method → result or error).
func imessageNode(t *testing.T, h *Hub, answer func(method string, params json.RawMessage) (any, error)) (*Machine, *[]string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	var mu sync.Mutex
	calls := &[]string{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		mu.Lock()
		*calls = append(*calls, method)
		mu.Unlock()
		return answer(method, params)
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: "mac", Name: "示例 Mac", Kind: "ssh"})
	m.peer = hubSide
	h.byID["mac"] = m
	h.machines = append(h.machines, m)
	return m, calls
}

const imessageChatsPage = `{"chats":[{"id":"imessage-0123456789ab","name":"张三","members":[{"handle":"+15550100","name":"张三"}],
"services":["SMS","iMessage"],"last":"2026-10-30T08:00:00Z","preview":"示例消息","unread":1}],"more":true,"before":"1.imessage-0123456789ab"}`

// The app reads a Mac's Messages through the hub: status with its steps, conversations, messages; the hub passes
// the pages along, refuses what is not a conversation id, and turns the node's failures into codes the app acts on.
func TestIMessageThroughTheHub(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	mux := h.routes()

	// Not set up: 404, and no connector in the list.
	if code, out := call(t, mux, "GET", "/api/v1/imessage", h.cfg.Token, ""); code != 404 || out["code"] != "not_configured" {
		t.Fatalf("not configured: %d %v", code, out)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/imessage/chats", h.cfg.Token, ""); code != 404 {
		t.Fatalf("chats not configured: %d", code)
	}

	permission := false
	_, calls := imessageNode(t, h, func(method string, params json.RawMessage) (any, error) {
		switch {
		case method == "connector.list" && permission:
			return json.RawMessage(`{"connectors":[{"id":"imessage","status":"ok","via":"ssh"}]}`), nil
		case method == "connector.list":
			return json.RawMessage(`{"connectors":[{"id":"imessage","status":"no_permission","error":"macOS 不让 Jarvis 读","guide":["一","二","三"],"via":"ssh"}]}`), nil
		case !permission:
			return nil, &rpc.Error{Code: proto.CodeNoPermission, Message: "denied"}
		case method == "connector.chats":
			var p struct {
				Limit  int    `json:"limit"`
				Before string `json:"before"`
			}
			_ = json.Unmarshal(params, &p)
			if p.Limit != 20 || p.Before != "9.imessage-0123456789ab" {
				return nil, fmt.Errorf("params %s", params)
			}
			return json.RawMessage(imessageChatsPage), nil
		case method == "connector.older":
			return nil, &rpc.Error{Code: proto.CodeNoChat, Message: "no such conversation"}
		}
		return nil, fmt.Errorf("unexpected %s", method)
	})
	h.cfg.IMessage = &imessage.Config{Machine: "mac"}
	if err := h.addIMessage(*h.cfg.IMessage); err != nil {
		t.Fatal(err)
	}
	h.imessage().Check(context.Background())

	// No Full Disk Access yet: the status has the steps, a read is 403.
	code, out := call(t, mux, "GET", "/api/v1/imessage", h.cfg.Token, "")
	if code != 200 || out["status"] != "no_permission" || len(out["guide"].([]any)) != 3 || out["machine"] != "示例 Mac" || out["via"] != "ssh" {
		t.Fatalf("status: %d %v", code, out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/imessage/chats?limit=20", h.cfg.Token, ""); code != 403 || out["code"] != proto.CodeNoPermission ||
		!strings.Contains(out["error"].(string), "完全磁盘访问权限") {
		t.Fatalf("chats without permission: %d %v", code, out)
	}

	permission = true
	h.imessage().Check(context.Background())
	_, out = call(t, mux, "GET", "/api/v1/connectors", h.cfg.Token, "")
	list := out["connectors"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != "imessage" || list[0].(map[string]any)["status"] != "ok" ||
		list[0].(map[string]any)["account"] != "示例 Mac" || list[0].(map[string]any)["name"] != "信息" {
		t.Fatalf("connectors: %v", out)
	}
	if caps := list[0].(map[string]any)["capabilities"].([]any); fmt.Sprint(caps) != "[status tools events inbox archive]" {
		t.Errorf("capabilities %v", caps)
	}
	if n := len(h.connectors.Tools(context.Background())); n != 3 {
		t.Errorf("%d tools with 交给 Jarvis on", n)
	}
	h.imessage().SetAI(false)
	if n := len(h.connectors.Tools(context.Background())); n != 0 {
		t.Errorf("%d tools with 交给 Jarvis off", n)
	}
	h.imessage().SetAI(true)

	code, out = call(t, mux, "GET", "/api/v1/imessage/chats?limit=20&before=9.imessage-0123456789ab", h.cfg.Token, "")
	chats, _ := out["chats"].([]any)
	if code != 200 || len(chats) != 1 || chats[0].(map[string]any)["name"] != "张三" || out["before"] != "1.imessage-0123456789ab" {
		t.Fatalf("chats: %d %v", code, out)
	}
	n := len(*calls)
	for path, want := range map[string]int{
		"/api/v1/imessage/chats/imessage-0123456789ab/messages": 404, // the Mac has no such conversation
		"/api/v1/imessage/chats/%2B15550100/messages":            404, // not an id: never sent to the Mac
	} {
		if code, out := call(t, mux, "GET", path, h.cfg.Token, ""); code != want || out["code"] != proto.CodeNoChat {
			t.Errorf("%s: %d %v", path, code, out)
		}
	}
	if len(*calls) != n+1 {
		t.Errorf("%d calls for two reads, one of them not an id", len(*calls)-n)
	}

	// Nothing about the messages in the log: the request log has paths (hashed ids) but no queries, and the only
	// number in it is the one this test sent in place of an id.
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "张三") || strings.Contains(line, "示例消息") || strings.Contains(line, "5550100") && !strings.Contains(line, "/chats/+15550100/") {
			t.Errorf("logged: %s", line)
		}
	}
}

// An old node, and a Mac that is not connected.
func TestIMessageOldOrOfflineMac(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	mux := h.routes()
	m, _ := imessageNode(t, h, func(method string, _ json.RawMessage) (any, error) {
		return nil, &rpc.Error{Message: "unknown method " + method}
	})
	if err := h.addIMessage(imessage.Config{Machine: "mac"}); err != nil {
		t.Fatal(err)
	}
	h.imessage().Check(context.Background())
	if _, out := call(t, mux, "GET", "/api/v1/imessage", h.cfg.Token, ""); out["status"] != "error" || !strings.Contains(out["error"].(string), "太旧") {
		t.Fatalf("old node: %v", out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/imessage/chats", h.cfg.Token, ""); code != 409 || out["code"] != proto.CodeNodeOutdated {
		t.Fatalf("old node chats: %d %v", code, out)
	}
	m.mu.Lock()
	m.peer = nil
	m.mu.Unlock()
	if code, out := call(t, mux, "GET", "/api/v1/imessage/chats", h.cfg.Token, ""); code != 503 || !strings.Contains(out["error"].(string), "不在线") {
		t.Fatalf("offline: %d %v", code, out)
	}
}

// 交给 Jarvis: on unless the user turned it off; the choice is kept and applied at once.
func TestIMessageAISwitch(t *testing.T) {
	h, _ := modelTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	mux := h.routes()
	imessageNode(t, h, func(method string, _ json.RawMessage) (any, error) {
		return json.RawMessage(`{"connectors":[{"id":"imessage","status":"ok"}]}`), nil
	})
	if err := h.addIMessage(imessage.Config{Machine: "mac"}); err != nil {
		t.Fatal(err)
	}
	if _, out := call(t, mux, "GET", "/api/v1/imessage", h.cfg.Token, ""); out["ai"] != true {
		t.Fatalf("default: %v", out)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/imessage/settings", h.cfg.Token, `{"ai":false}`); code != 200 || out["ai"] != false {
		t.Fatalf("off: %d %v", code, out)
	}
	if h.imessage().AI() || len(h.connectors.Tools(context.Background())) != 0 {
		t.Error("still on")
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/imessage/settings", h.cfg.Token, `{}`); code != 400 {
		t.Errorf("no ai: %d", code)
	}
	// A restart reads the choice back.
	h.connectors = connector.NewRegistry(nil)
	if err := h.addIMessage(imessage.Config{Machine: "mac"}); err != nil {
		t.Fatal(err)
	}
	if h.imessage().AI() {
		t.Error("on again after a restart")
	}
	if code, out := call(t, mux, "PUT", "/api/v1/imessage/settings", h.cfg.Token, `{"ai":true}`); code != 200 || out["ai"] != true || !h.imessage().AI() {
		t.Errorf("on: %d %v", code, out)
	}
}

// The Mac's new messages go into the knowledge base as chats imessage-<the conversation's hash>, people as
// imessage-<hash of their number>: no number in a file name; the cursor moves on.
func TestIMessageArchive(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	imessageNode(t, h, func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "connector.list":
			return json.RawMessage(`{"connectors":[{"id":"imessage","status":"ok"}]}`), nil
		case "connector.changes":
			return json.RawMessage(`{"next":"9","batches":[{"chat":{"id":"imessage-0123456789ab","name":"张三","members":[{"handle":"+15550100","name":"张三"}],
"services":["SMS"],"last":"` + time.Now().UTC().Format(time.RFC3339) + `"},"messages":[
{"id":"8","at":"` + time.Now().Add(-time.Minute).UTC().Format(time.RFC3339) + `","kind":"message","sender":{"handle":"+15550100","name":"张三"},"text":"示例：周五见"},
{"id":"9","at":"` + time.Now().UTC().Format(time.RFC3339) + `","kind":"message","from_me":true,"text":"好"}]}]}`), nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	})
	if err := h.addIMessage(imessage.Config{Machine: "mac"}); err != nil {
		t.Fatal(err)
	}
	c := h.imessage()
	c.Check(context.Background())
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &kb.Engine{Store: st}
	n, err := archiveConnector(context.Background(), e, imessage.ID, c)
	if err != nil || n != 2 {
		t.Fatalf("%d lines, %v", n, err)
	}
	chat, ok := st.Chat("imessage-0123456789ab")
	if !ok || chat.Name != "张三" || chat.Kind != "dm" || chat.Pending != 2 {
		t.Fatalf("chat %+v", chat)
	}
	people := st.People()
	if len(people) != 1 || people[0].ID != "imessage-"+imessage.PersonKey("+15550100") || people[0].Name != "张三" {
		t.Fatalf("people %+v", people)
	}
	if raw, _ := os.ReadFile(filepath.Join(st.Dir, "cursors", "imessage.json")); !strings.Contains(string(raw), `"9"`) {
		t.Errorf("cursor %s", raw)
	}
	var names []string
	_ = filepath.WalkDir(st.Dir, func(p string, _ fs.DirEntry, _ error) error {
		names = append(names, p)
		return nil
	})
	if strings.Contains(strings.Join(names, "\n"), "5550100") {
		t.Errorf("a number in a file name:\n%s", strings.Join(names, "\n"))
	}
}
