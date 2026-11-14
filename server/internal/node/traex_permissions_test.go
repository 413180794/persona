package node

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestTraexPermissionArgs(t *testing.T) {
	for _, tc := range []struct {
		mode, effort string
		want         []string
	}{
		{"", "", nil},
		{"", effortAuto, nil},
		{"plan", "", []string{"--sandbox", "read-only", "--ask-for-approval", "never"}},
		{proto.ModeBypass, "", []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}},
		{"default", "", []string{"--permission-mode", "default"}},
		{"acceptEdits", "", []string{"--permission-mode", "default"}},
		{"auto", "", []string{"--permission-mode", "auto"}},
		{"plan", "high", []string{"--sandbox", "read-only", "--ask-for-approval", "never", "--config", "model_reasoning_effort=high"}},
	} {
		t.Run(tc.mode+"/"+tc.effort, func(t *testing.T) {
			got, err := traexPermissionArgs(tc.mode, tc.effort)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("args = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh"} {
		got, err := traexPermissionArgs("", effort)
		if err != nil || !slices.Equal(got, []string{"--config", "model_reasoning_effort=" + effort}) {
			t.Fatalf("effort %s: %q (%v)", effort, got, err)
		}
	}
	for _, tc := range []struct{ mode, effort string }{{"unknown", ""}, {"plan", "max"}, {"plan", "ultra"}} {
		if got, err := traexPermissionArgs(tc.mode, tc.effort); err == nil || got != nil {
			t.Fatalf("unsupported options %v: %q (%v)", tc, got, err)
		}
	}
}

func TestTraexReadonlyArguments(t *testing.T) {
	for _, args := range [][]string{
		{"-y"}, {"-ymodel"}, {"-sy"}, {"--dangerously-bypass-approvals-and-sandbox"}, {"--yolo"}, {"--full-auto"},
		{"--sandbox", "workspace-write"}, {"--sandbox=read-only"}, {"-s", "read-only"}, {"-sread-only"}, {"-s=read-only"},
		{"--ask-for-approval", "never"}, {"--ask-for-approval=never"}, {"-a", "never"}, {"-anever"}, {"-a=never"},
		{"--permission-mode", "default"}, {"--permission-mode=bypass_permissions"},
		{"--"}, {"resume", threadOld}, {"--resume", threadOld}, {"exec"}, {"inspect this"},
		{"--config", "sandbox_mode=\"danger-full-access\""}, {"--config=approval_policy=\"on-request\""},
		{"-cpermissions.default=\"bypass_permissions\""}, {"-c", "sandbox_permissions=[\"disk-full-write-access\"]"},
		{"--remote", "ws://127.0.0.1:9"}, {"--add-dir", "/tmp"}, {"--unknown"}, {"-c"}, {"-m"}, {"--model"},
	} {
		if !traexReadonlyConflict(args) {
			t.Errorf("read-only accepted %q", args)
		}
	}
	for _, args := range [][]string{
		nil, {"--model", "-sample"}, {"--model=--sandbox"}, {"-m-y"}, {"-p", "sample"},
		{"--config", "model_reasoning_effort=low"}, {"-cmodel_reasoning_effort=high"}, {"-c=model_reasoning_effort=xhigh"},
		{"--enable", "sample-feature", "--no-daemon"},
	} {
		if traexReadonlyConflict(args) {
			t.Errorf("read-only refused safe option values %q", args)
		}
	}
}

// Keep launcher probes in an isolated rc file; the CLI itself is never run by these tests.
func traexPermissionShell(t *testing.T, home, definitions string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("ZDOTDIR", home)
	writeFile(t, filepath.Join(home, ".bashrc"), definitions+"\n")
	withCodexLaunchers(t, map[string][]string{})
}

func traexPermissionNode(t *testing.T, id string) (*Node, *fakeHerdr, string) {
	t.Helper()
	n, f, home := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.after(func(c herdrCall) {
		if c.Method == "pane.send_input" {
			f.reply("session.snapshot", snapshot(nil, nil, []any{agentOf(traexAgent, id, proto.StatusIdle, "w1:p2", "w1:t2", "w1", home, "")}, nil))
		}
	})
	return n, f, home
}

func traexPermissionCommand(t *testing.T, f *fakeHerdr, command string) []string {
	t.Helper()
	var found [][]string
	for _, c := range f.calls() {
		if c.Method != "pane.send_input" {
			continue
		}
		words := shellWords(str(c.Params["text"]))
		if i := slices.Index(words, command); i >= 0 {
			found = append(found, words[i:])
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d start commands for %s: %q", len(found), command, found)
	}
	return found[0]
}

func TestTraexSessionStartPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, launcher, agent, mode, definitions string
		managed                                  bool
	}{
		{"readonly", traexAgent, "", "plan", "", false},
		{"writable", traexAgent, "", proto.ModeBypass, "", false},
		{"typed-alias", "review_wrapper", traexAgent, "plan", "alias review_wrapper='traex --model sample'", false},
		{"typed-function", "review_wrapper", traexAgent, "plan", "review_wrapper() { traex --model sample \"$@\"; }", false},
		{"managed-readonly", "managed-traex", "", "plan", "", true},
		{"managed-writable", "managed-traex", "", proto.ModeBypass, "", true},
		{"launcher-default", "review_wrapper", traexAgent, "", "alias review_wrapper='traex -y'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, home := traexPermissionNode(t, threadNew)
			traexPermissionShell(t, home, tc.definitions)
			prefix := []string{tc.launcher}
			if tc.managed {
				n.setManaged([]proto.ManagedLauncher{{Name: tc.launcher, Agent: traexAgent, Args: []string{"--model", "sample"}}})
				prefix = []string{n.bin, "launch", tc.launcher}
			}
			params := map[string]any{
				"cwd": home, "workspace_id": "w1", "launcher": tc.launcher, "agent_type": tc.agent,
				"permission_mode": tc.mode, "prompt": "--dangerously-bypass-approvals-and-sandbox", "async": true,
			}
			if tc.agent == "" {
				delete(params, "agent_type")
			}
			if tc.mode != "" {
				params["model"], params["effort"] = "sample-model", "high"
			}
			res, err := request(t, n, "session.start", params)
			if err != nil {
				t.Fatal(err)
			}
			session := decodeSessions(t, []any{res["session"]})[0]
			if session.SessionID != threadNew || session.Agent != traexAgent || session.Starting || session.PermissionMode != tc.mode {
				t.Fatalf("started session = %+v", session)
			}
			args, err := traexPermissionArgs(tc.mode, str(params["effort"]))
			if err != nil {
				t.Fatal(err)
			}
			if tc.mode != "" {
				args = append(args, "--model", "sample-model", "--")
			}
			want := append(slices.Clone(prefix), args...)
			want = append(want, str(params["prompt"]))
			if got := traexPermissionCommand(t, f, prefix[0]); !slices.Equal(got, want) {
				t.Fatalf("start command = %q, want %q", got, want)
			}
			if st, _ := n.state.get(threadNew); st.AppMode != tc.mode {
				t.Fatalf("saved mode = %q, want %q", st.AppMode, tc.mode)
			}
			restoreArgs, _ := traexPermissionArgs(tc.mode, "")
			restore := append(slices.Clone(prefix), restoreArgs...)
			restore = append(restore, "resume", threadNew)
			assertTraexHerdrResumeReport(t, f, restore)
		})
	}
}

func TestTraexReadonlyManagedStartChecksPulledArguments(t *testing.T) {
	n, hh, oh := startNode(t)
	entry := proto.ManagedLauncher{Name: "managed-traex", Agent: traexAgent, Delivery: proto.DeliveryOnDemand, Withheld: true, Rev: "r1"}
	hub := &pullHub{answer: func(name string) (any, error) {
		return map[string]any{"launcher": proto.ManagedLauncher{Name: name, Agent: traexAgent, Args: []string{"-y"}}}, nil
	}}
	withPullHub(t, n, hub)
	n.setCatalog([]proto.ManagedLauncher{entry})
	_, err := request(t, n, "session.start", map[string]any{
		"cwd": t.TempDir(), "launcher": entry.Name, "permission_mode": "plan", "async": true,
	})
	if code, data := refusalOf(t, err); code != "readonly_unsupported" || data["delivered"] != deliveredNone {
		t.Fatalf("pulled read-only conflict: %v (%v)", err, data)
	}
	if hub.count() != 1 || hh.asked()+oh.asked() != 0 {
		t.Fatalf("pulls = %d, terminal calls = %d", hub.count(), hh.asked()+oh.asked())
	}
}

func TestTraexReadonlyDirectResumeKeepsSavedPermissions(t *testing.T) {
	n, f, home := traexPermissionNode(t, threadOld)
	traexPermissionShell(t, home, "")
	n.state.put(threadOld, sessionState{Launcher: traexAgent, Cwd: home, AppMode: "plan"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := n.startOtherAgent(ctx, n.hosts.Get(host.Herdr).(*herdrHost), home, traexAgent, traexAgent, "Inspect the project", threadOld, "w1", "")
	if err != nil || s.SessionID != threadOld {
		t.Fatalf("direct resume = %+v (%v)", s, err)
	}
	want := []string{traexAgent, "--sandbox", "read-only", "--ask-for-approval", "never", "resume", threadOld, "--", "Inspect the project"}
	if got := traexPermissionCommand(t, f, traexAgent); !slices.Equal(got, want) {
		t.Fatalf("direct resume command = %q, want %q", got, want)
	}
}

func TestTraexReadonlySessionStartRefusesConflictingLaunchers(t *testing.T) {
	for _, tc := range []struct {
		name, launcher, definitions string
		managedArgs                 []string
	}{
		{"native-alias", traexAgent, "alias traex='traex -y'", nil},
		{"alias", "review_wrapper", "alias review_wrapper='traex --permission-mode bypass_permissions'", nil},
		{"delimiter", "review_wrapper", "alias review_wrapper='traex --'", nil},
		{"drops-options", "review_wrapper", "review_wrapper() { traex --model sample; }", nil},
		{"managed-bypass", "managed-traex", "", []string{"-y"}},
		{"managed-preset", "managed-traex", "", []string{"--permission-mode=default"}},
		{"managed-config", "managed-traex", "", []string{"-c", "sandbox_mode=\"danger-full-access\""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, hh, oh := startNode(t)
			home := t.TempDir()
			traexPermissionShell(t, home, tc.definitions)
			if tc.managedArgs != nil {
				n.setManaged([]proto.ManagedLauncher{{Name: tc.launcher, Agent: traexAgent, Args: tc.managedArgs}})
			}
			_, err := request(t, n, "session.start", map[string]any{
				"cwd": home, "launcher": tc.launcher, "agent_type": traexAgent, "permission_mode": "plan",
			})
			if code, data := refusalOf(t, err); code != "readonly_unsupported" || data["delivered"] != deliveredNone {
				t.Fatalf("readonly conflict: %v (%v)", err, data)
			}
			if hh.asked()+oh.asked() != 0 {
				t.Fatal("a refused read-only start reached the terminal")
			}
		})
	}
}

func TestTraexSessionResumePermissions(t *testing.T) {
	for _, tc := range []struct{ mode, saved string }{{"plan", ""}, {proto.ModeBypass, ""}, {"", "plan"}} {
		t.Run(tc.mode+"/"+tc.saved, func(t *testing.T) {
			n, f, home := traexPermissionNode(t, threadOld)
			traexPermissionShell(t, home, "")
			n.state.put(threadOld, sessionState{Launcher: traexAgent, Cwd: home, AppMode: tc.saved})
			res, err := request(t, n, "session.start", map[string]any{
				"cwd": home, "workspace_id": "w1", "launcher": traexAgent, "permission_mode": tc.mode,
				"resume_session_id": threadOld, "prompt": "Inspect the project",
			})
			if err != nil {
				t.Fatal(err)
			}
			if s := decodeSessions(t, []any{res["session"]})[0]; s.SessionID != threadOld {
				t.Fatalf("resumed session = %+v", s)
			}
			mode := tc.mode
			if mode == "" {
				mode = tc.saved
			}
			args, _ := traexPermissionArgs(mode, "")
			want := append([]string{traexAgent}, args...)
			want = append(want, "resume", threadOld, "--", "Inspect the project")
			if got := traexPermissionCommand(t, f, traexAgent); !slices.Equal(got, want) {
				t.Fatalf("resume command = %q, want root options before resume: %q", got, want)
			}
		})
	}
}

func TestTraexReadonlyLiveResumeRefused(t *testing.T) {
	for _, agent := range []string{traexAgent, "claude"} {
		t.Run(agent, func(t *testing.T) {
			n, hh, oh := startNode(t)
			home := t.TempDir()
			traexPermissionShell(t, home, "")
			n.mu.Lock()
			n.agents[threadOld] = host.Agent{Agent: traexAgent, Cwd: home, PaneID: "w1:p1", AgentStatus: proto.StatusIdle}
			n.mu.Unlock()
			_, err := request(t, n, "session.start", map[string]any{
				"cwd": home, "launcher": traexAgent, "agent_type": agent, "permission_mode": "plan", "resume_session_id": threadOld,
			})
			if code, data := refusalOf(t, err); code != "readonly_unsupported" || data["delivered"] != deliveredNone {
				t.Fatalf("live resume: %v (%v)", err, data)
			}
			if hh.asked()+oh.asked() != 0 {
				t.Fatal("a refused live resume reached the terminal")
			}
		})
	}
}

func TestTraexUnsupportedEffortRefusedBeforeTerminal(t *testing.T) {
	n, hh, oh := startNode(t)
	_, err := request(t, n, "session.start", map[string]any{
		"cwd": t.TempDir(), "launcher": traexAgent, "permission_mode": "plan", "effort": "max",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported effort") || hh.asked()+oh.asked() != 0 {
		t.Fatalf("unsupported effort: %v, terminal calls %d", err, hh.asked()+oh.asked())
	}
}

func TestTraexFreshStartExcludesHistoricalRollouts(t *testing.T) {
	for _, source := range []string{"rollout", "paginated"} {
		t.Run(source, func(t *testing.T) {
			id := threadOld
			if source == "paginated" {
				id = threadMain
			}
			n, f, home := traexPermissionNode(t, id)
			traexPermissionShell(t, home, "")
			if source == "rollout" {
				writeMetaRollout(t, n.traexHome(), id, `{"id":"`+id+`","cwd":"`+home+`","source":"cli"}`, time.Now().Add(-time.Hour))
			} else {
				traexHistoryFixture(t, n, nil)
				if _, ok := n.traexThreads()[id]; !ok {
					t.Fatal("fixture has no paginated thread to exclude")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 1300*time.Millisecond)
			defer cancel()
			s, err := n.start(ctx, home, traexAgent, "Inspect the project", "", "w1", "plan", "", "", "", "")
			if s != nil || err == nil || !strings.Contains(err.Error(), "wait for traex session") {
				t.Fatalf("a fresh start adopted old history: %+v (%v)", s, err)
			}
			traexPermissionCommand(t, f, traexAgent)
		})
	}
}
