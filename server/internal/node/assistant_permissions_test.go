package node

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestAssistantAlwaysRechecksHub(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	const session = "55555555-5555-4555-8555-555555555555"
	n.procs[session] = &assistantProc{id: session, agent: "claude", exited: make(chan struct{})}
	allowed := true
	fh.answer = func(proto.Interaction) proto.Resolution {
		return proto.Resolution{Allow: &allowed, Always: allowed}
	}
	for i, command := range []string{"ls /tmp", "cat /tmp/settings.json"} {
		fh.mu.Lock()
		allowed = i == 0
		fh.mu.Unlock()
		raw, _ := json.Marshal(hookInput{HookEventName: "PermissionRequest", SessionID: session,
			ToolName: "Bash", ToolUseID: command, ToolInput: json.RawMessage(`{"command":"` + command + `"}`)})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := n.handleHook(ctx, sockRequest{Input: raw, Attempt: "a1"})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(out)
		want := `"behavior":"allow"`
		if i == 1 {
			want = `"behavior":"deny"`
		}
		if !strings.Contains(string(b), want) || strings.Contains(string(b), "updatedPermissions") {
			t.Fatalf("call %d: %s", i, b)
		}
		cards := fh.interactions()
		if len(cards) != i+1 || !slices.Equal(cards[i].AlwaysKeys, []string{"tool:Bash"}) {
			t.Fatalf("call %d did not consult hub with the existing Bash key: %+v", i, cards)
		}
	}
}

func TestAssistantLarkAlwaysRechecksHub(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	const session = "66666666-6666-4666-8666-666666666666"
	n.procs[session] = &assistantProc{id: session, exited: make(chan struct{})}
	n.larkCfg, n.larkCfgAt = olCfg(), time.Now().Add(time.Hour)
	// A grant cached before the upgrade must not bypass the hub either.
	n.rememberLarkAlways(session, "POST /conversations/*/messages")
	allow := true
	fh.answer = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &allow, Always: true} }
	ix := proto.Interaction{SessionID: session, ExpiresAt: time.Now().Add(time.Minute)}
	for i, want := range []string{"allow", "deny"} {
		fh.mu.Lock()
		allow = i == 0
		fh.mu.Unlock()
		d, _, err := n.larkCheck(context.Background(), session, t.TempDir(), `ex POST /conversations/7123/messages '{"text":"hello"}'`, ix)
		if err != nil || d != want || len(fh.interactions()) != i+1 {
			t.Fatalf("call %d: decision %q, err %v, cards %d", i, d, err, len(fh.interactions()))
		}
	}
}

func TestAssistantCodexAlwaysStaysAtHub(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	p, c, f := newFakeCodex(t, n, t.TempDir())
	p.id, c.threadID, c.turnID = testThread, testThread, "turn-1"
	fh.answer = func(proto.Interaction) proto.Resolution {
		allow := true
		return proto.Resolution{Allow: &allow, Always: true}
	}
	for i, method := range []string{"item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval"} {
		res := f.request(i, method, map[string]any{"threadId": testThread, "turnId": "turn-1", "itemId": "item",
			"command": "cat /tmp/settings.json", "grantRoot": "/tmp", "permissions": map[string]any{"network": true}})
		if method == "item/permissions/requestApproval" {
			if res["scope"] != "turn" {
				t.Fatalf("persistent permission scope: %v", res)
			}
		} else if res["decision"] != "accept" {
			t.Fatalf("%s: persistent decision %v", method, res)
		}
	}
}

func TestResetAssistantPermissionsWaitsForExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell process fixture")
	}
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			n := codexTestNode(t)
			const session = "77777777-7777-4777-8777-777777777777"
			cmd := exec.Command("/bin/sh", "-c", "read line")
			cmd.SysProcAttr = osproc.GroupAttr()
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			p := &assistantProc{id: session, agent: agent, cmd: cmd, stdin: stdin, exited: make(chan struct{})}
			n.procs[session] = p
			go func() { _ = cmd.Wait(); close(p.exited) }()
			t.Cleanup(func() { p.stop() })
			n.state.put(session, sessionState{Cwd: filepath.Join(n.home, ".persona", "assistant")})
			n.rememberLarkAlways(session, "POST /messages")
			n.rememberLarkAlways("other", "POST /messages")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			restart, err := n.resetAssistantPermissions(ctx, session)
			if err != nil || restart || p.alive() {
				t.Fatalf("reset returned before exit: restart %v, err %v, alive %v", restart, err, p.alive())
			}
			if n.larkAlways(session, "POST /messages") || !n.larkAlways("other", "POST /messages") {
				t.Fatal("reset did not isolate the conversation's cached grants")
			}
			delete(n.procs, session)
			if restart, err := n.resetAssistantPermissions(ctx, session); err != nil || restart {
				t.Fatalf("reset inactive conversation: %v %v", restart, err)
			}
		})
	}
}

func TestResetAssistantPermissionsRuntimeNeedsRestart(t *testing.T) {
	n, hub := runtimeNode(t)
	assistantHub(hub)
	host := dialRuntime(t, n, assistantRuntimeHello())
	sid := startedAssistant(t, n, host)
	raw, err := nodeCall(t, n, "assistant.permissions.reset", map[string]any{"session_id": sid})
	if err != nil || !strings.Contains(string(raw), `"restart_required":true`) {
		t.Fatalf("runtime reset must disclose the limitation: %s %v", raw, err)
	}
	if len(host.got("session.close")) != 0 || len(host.got("session.interrupt")) != 0 {
		t.Fatal("runtime interrupted or destroyed instead of reporting the unsupported reset")
	}
}

func TestResetAssistantPermissionsRejectsTerminal(t *testing.T) {
	n := codexTestNode(t)
	if _, err := nodeCall(t, n, "assistant.permissions.reset", map[string]any{"session_id": "88888888-8888-4888-8888-888888888888"}); err == nil {
		t.Fatal("reset accepted a conversation the node does not own")
	}
}

// The launcher can exit on TERM while its child still owns cached rules. The reset must kill
// that child too; a pipe owned by both processes reaches EOF only after the whole group exits.
func TestResetAssistantPermissionsKillsUncooperativeChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group signal fixture")
	}
	n := codexTestNode(t)
	const session = "99999999-9999-4999-8999-999999999999"
	out, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	defer writer.Close()
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' TERM; /bin/sh -c 'trap "" TERM; echo ready; while :; do sleep 30; done' & wait`)
	cmd.SysProcAttr = osproc.GroupAttr()
	cmd.Stdout = writer
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	p := &assistantProc{id: session, agent: "claude", cmd: cmd, stdin: stdin, exited: make(chan struct{})}
	n.procs[session] = p
	go func() { _ = cmd.Wait(); close(p.exited) }()
	t.Cleanup(func() { _ = osproc.StopGroup(cmd.Process.Pid, true) })
	_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(out)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child startup: %q %v", line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if restart, err := n.resetAssistantPermissions(ctx, session); err != nil || restart {
		t.Fatalf("reset: restart %v, err %v", restart, err)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("cached-rule child survived reset: %v", err)
	}
}
