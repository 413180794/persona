package node

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

const decidedSession = "33333333-3333-4333-8333-333333333333"

func decidedInput(cmd string) hookInput {
	b, _ := json.Marshal(map[string]any{"command": cmd, "description": "remove the views"})
	return hookInput{HookEventName: "PermissionRequest", SessionID: decidedSession, ToolName: "Bash", ToolInput: b,
		PermissionMode: "bypassPermissions"}
}

func runPermissionHook(t *testing.T, n *Node, in hookInput) (out any, took time.Duration) {
	t.Helper()
	raw, _ := json.Marshal(in)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	out, err := n.handleHook(ctx, sockRequest{Input: raw, Attempt: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	return out, time.Since(start)
}

// A call another hook answered (#2648) gets no card: on a machine where hooks say so, a card waits a moment first.
func TestAnotherHookAnswersFirst(t *testing.T) {
	defer func(g time.Duration) { permissionGrace = g }(permissionGrace)
	permissionGrace = 300 * time.Millisecond
	n := codexTestNode(t)
	fh := withFakeHub(t, n)

	// No hook has said anything here: the card goes out at once, as before.
	if out, took := runPermissionHook(t, n, decidedInput("ls")); out == nil || took > 200*time.Millisecond || len(fh.interactions()) != 1 {
		t.Fatalf("before any hook.decided: out %v after %v, %d cards", out, took, len(fh.interactions()))
	}

	// Another hook answers while the card waits: nothing is shown, nothing is answered.
	n.decided.decide(decidedInput("echo earlier"), time.Now())
	in := decidedInput("cd view && rm -f sub/*")
	go func() {
		time.Sleep(20 * time.Millisecond)
		// The same input, written with other spacing: the key compacts it.
		other := in
		other.ToolInput = json.RawMessage(`{ "command": "cd view && rm -f sub/*", "description": "remove the views" }`)
		n.decided.decide(other, time.Now())
	}()
	out, took := runPermissionHook(t, n, in)
	if out != nil || len(fh.interactions()) != 1 || took >= permissionGrace {
		t.Fatalf("answered by another hook: out %v after %v, %d cards", out, took, len(fh.interactions()))
	}
	// An answer that came before the card asked: no card either.
	if out, _ := runPermissionHook(t, n, in); out != nil || len(fh.interactions()) != 1 {
		t.Fatalf("answered before: out %v, %d cards", out, len(fh.interactions()))
	}
	// Nobody answers this one: after the grace the card goes out.
	if out, took := runPermissionHook(t, n, decidedInput("rm -rf \"$t\"")); out == nil || took < permissionGrace || len(fh.interactions()) != 2 {
		t.Fatalf("not answered: out %v after %v, %d cards", out, took, len(fh.interactions()))
	}
	// Another subagent's call with the same input is another call.
	sub := in
	sub.AgentID = "a1b2"
	if out, _ := runPermissionHook(t, n, sub); out == nil || len(fh.interactions()) != 3 {
		t.Fatalf("a subagent's: out %v, %d cards", out, len(fh.interactions()))
	}
}

// A card already on the phone when the other hook's word comes is withdrawn.
func TestAnotherHookAnswersAfterTheCard(t *testing.T) {
	defer func(g time.Duration) { permissionGrace = g }(permissionGrace)
	permissionGrace = 10 * time.Millisecond
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	release := make(chan struct{})
	defer close(release)
	fh.mu.Lock()
	fh.answer = func(proto.Interaction) proto.Resolution { <-release; return proto.Resolution{} }
	fh.mu.Unlock()
	in := decidedInput("cd view && rm -f sub/*")
	done := make(chan any, 1)
	go func() { out, _ := runPermissionHook(t, n, in); done <- out }()
	for deadline := time.Now().Add(3 * time.Second); len(fh.interactions()) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("no card")
		}
		time.Sleep(5 * time.Millisecond)
	}
	n.decided.decide(in, time.Now())
	select {
	case out := <-done:
		if out != nil {
			t.Fatalf("withdrawn card answered: %v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the hook still waits for the phone")
	}
}

// hook.decided on the socket: only a PermissionRequest of a real session counts.
func TestHookDecidedOnTheSocket(t *testing.T) {
	n := codexTestNode(t)
	send := func(in hookInput) map[string]any {
		t.Helper()
		a, b := net.Pipe()
		defer a.Close()
		go n.serveSock(context.Background(), b)
		raw, _ := json.Marshal(in)
		line, _ := json.Marshal(sockRequest{Kind: "hook.decided", Input: raw})
		_, _ = a.Write(append(line, '\n'))
		resp, err := bufio.NewReader(a).ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(resp, &m)
		return m
	}
	bad := decidedInput("ls")
	bad.HookEventName = "PreToolUse"
	if r := send(bad); r["ok"] != true || n.decided.answered(permissionKey(bad)) {
		t.Fatalf("a PreToolUse counted: %v", r)
	}
	in := decidedInput("ls")
	if r := send(in); r["ok"] != true || !n.decided.answered(permissionKey(in)) {
		t.Fatalf("not recorded: %v", r)
	}
}

func TestDecisionsForget(t *testing.T) {
	var d decisions
	old, now := decidedInput("old"), time.Now()
	d.decide(old, now.Add(-decidedKeep-time.Second))
	d.decide(decidedInput("new"), now)
	if d.answered(permissionKey(old)) || !d.answered(permissionKey(decidedInput("new"))) {
		t.Fatal("old answers are forgotten, new ones kept")
	}
}
