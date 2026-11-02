package node

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestCodexExactModelIsSentOnStartResumeAndTurn(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "resume"}[resume], func(t *testing.T) {
			n := codexTestNode(t)
			withFakeHub(t, n)
			cwd := t.TempDir()
			p, c, f := newFakeCodex(t, n, cwd)
			p.model = modelExecution{Model: "specific-model"}
			method := "thread/start"
			id := ""
			if resume {
				method = "thread/resume"
				id = testThread
			}
			f.results[method] = map[string]any{"thread": map[string]any{"id": testThread}}
			f.results["turn/start"] = map[string]any{"turn": map[string]any{"id": "turn"}}
			if err := c.start(context.Background(), id, cwd, resume); err != nil {
				t.Fatal(err)
			}
			if err := c.deliver(context.Background(), "", "hello", nil); err != nil {
				t.Fatal(err)
			}
			for _, m := range []string{method, "turn/start"} {
				var params map[string]any
				_ = json.Unmarshal(f.sent(m)[0].Params, &params)
				if params["model"] != "specific-model" {
					t.Fatalf("%s did not receive model: %+v", m, params)
				}
			}
		})
	}
}

func TestModelFingerprintTracksManagedProviderChanges(t *testing.T) {
	n, _ := testNode(t)
	option := modelExecution{Model: "specific", Fingerprint: "binding"}
	n.setManaged([]proto.ManagedLauncher{{Name: "provider", Model: "default", APIKey: "key-one"}})
	first := n.executionFingerprint("provider", option)
	n.setManaged([]proto.ManagedLauncher{{Name: "provider", Model: "default", APIKey: "key-two"}})
	if second := n.executionFingerprint("provider", option); first == second {
		t.Fatal("credential change reused old process")
	}
	if first == n.executionFingerprint("provider", modelExecution{Model: "different", Fingerprint: "binding"}) {
		t.Fatal("model change reused old process")
	}
}

func TestModelChangeDoesNotKillBackgroundTasks(t *testing.T) {
	n := codexTestNode(t)
	withFakeHub(t, n)
	old := modelExecution{Model: "old", Fingerprint: "before"}
	p := &assistantProc{id: testThread, agent: "claude", launcher: "claude", cwd: t.TempDir(), exited: make(chan struct{}), born: time.Now(), model: old, fingerprint: n.executionFingerprint("claude", old), tasks: []string{"background work"}}
	n.procs[testThread] = p
	n.state.put(testThread, sessionState{Launcher: "claude", Cwd: p.cwd, StartedAt: time.Now().UTC(), Model: "old", ModelFingerprint: "before", Tasks: p.tasks})
	_, err := n.assistantSend(context.Background(), testThread, p.cwd, "claude", "hello", nil, "", "", modelExecution{Model: "new", Fingerprint: "after"})
	if err == nil || !strings.Contains(err.Error(), "当前任务结束") {
		t.Fatalf("model change was not held: %v", err)
	}
	if !p.alive() || n.procs[testThread] != p {
		t.Fatal("background process killed")
	}
	state, _ := n.state.get(testThread)
	if state.Model != "old" || len(state.Tasks) != 1 {
		t.Fatalf("rejected send changed state: %+v", state)
	}
}
