package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestTaskExecutionManagedEligibilityBeforeSend(t *testing.T) {
	for _, target := range []string{"compatible", "work", "managed-codex", "managed-runtime"} {
		t.Run(target, func(t *testing.T) {
			f := newFakeHub().withMachine()
			f.sessions["remote"] = nil
			if target == "work" {
				f.hello.Assistant.WorkMachines = []string{"remote"}
			}
			s := newTestService(t, f)
			agent := "claude"
			switch target {
			case "managed-codex":
				agent = "codex"
			case "managed-runtime":
				agent = "dsh"
			}
			putLaunchers(t, s, storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "managed", Agent: agent, BaseURL: "https://provider.example"}, Share: shareAll, ScopeMachines: []string{"remote"}})
			main := ModelBinding{Provider: "launcher.managed", Model: "selected-model", Effort: "high"}
			setModel(t, s, "assistant", main)
			if b, err := s.taskModelBaseline(context.Background(), "remote"); err != nil || b != main {
				t.Fatalf("fixture did not retain the managed baseline: %+v %v", b, err)
			}
			setTasks(s, Task{ID: "job", Prompt: "Check files", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true, ExecutionMachineID: "remote"})
			_, err := runNow(t, s, "job")
			if target == "compatible" {
				if err != nil {
					t.Fatal(err)
				}
				if p := f.lastSend(t); p["launcher"] != "managed" || p["model"] != "selected-model" || p["effort"] != "high" {
					t.Fatalf("managed binding was replaced: %v", p)
				}
			} else if err == nil || f.sent() != 0 {
				t.Fatalf("ineligible managed target sent: error=%v sends=%d", err, f.sent())
			}
			if got, err := s.resolveModel(context.Background(), "assistant"); err != nil || got != main {
				t.Fatalf("task changed the assistant default: %+v %v", got, err)
			}
		})
	}
}

func TestTaskExecutionSavedLauncherCatalogAuthority(t *testing.T) {
	for _, catalog := range []string{"empty", "removed", "incompatible", "unknown", "omitted", "null", "unknown-managed", "legacy", "configured"} {
		t.Run(catalog, func(t *testing.T) {
			f := newFakeHub().withMachine()
			switch catalog {
			case "empty", "configured", "unknown-managed":
				f.launcherList = []proto.Launcher{}
			case "removed":
				f.launcherList = []proto.Launcher{{Name: "other", Source: "zshrc"}}
			case "incompatible":
				f.launcherList = []proto.Launcher{{Name: "saved", Source: "dsh", Agent: "dsh", Assistant: false}}
			case "legacy":
				f.launcherList = []proto.Launcher{{Name: "saved"}} // older nodes omit source and agent
			}
			if catalog == "configured" {
				f.hello.Models.Launchers = map[string][]ConfiguredLauncher{"server": {{Launcher: proto.Launcher{Name: "saved"}}}}
			}
			if catalog == "omitted" || catalog == "null" {
				f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
					if catalog == "null" {
						return map[string]any{"launchers": nil}, nil
					}
					return map[string]any{}, nil
				})
			}
			st := &memStore{}
			s := newServiceOn(t, f, st)
			setTasks(s, Task{ID: "job", Prompt: "Check files", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
			setModel(t, s, "task.job", ModelBinding{Provider: "launcher.saved"})
			if catalog == "unknown-managed" {
				st.failKey(launchersManagedKey, errors.New("provider catalog unavailable"))
			}
			_, err := runNow(t, s, "job")
			allowed := catalog == "unknown" || catalog == "omitted" || catalog == "null" || catalog == "unknown-managed" || catalog == "legacy" || catalog == "configured"
			if allowed {
				if err != nil {
					t.Fatal(err)
				}
				if p := f.lastSend(t); p["launcher"] != "saved" {
					t.Fatalf("saved launcher was substituted: %v", p)
				}
			} else if err == nil || f.sent() != 0 {
				t.Fatalf("authoritative unavailable launcher sent: error=%v sends=%d", err, f.sent())
			}
			if !allowed {
				r := request(s, "PUT", "/api/v1/assistant/tasks/job", `{"title":"Edited","prompt":"Check files","repeat":"interval","every_minutes":30,"enabled":true,"launcher":"saved","model":"new-model"}`)
				if r.Code != http.StatusBadRequest || f.sent() != 0 {
					t.Fatalf("unavailable launcher edit: status=%d sends=%d body=%s", r.Code, f.sent(), r.Body.String())
				}
				if got, err := s.resolveModel(context.Background(), "task.job"); err != nil || got.Provider != "launcher.saved" || got.Model != "" {
					t.Fatalf("rejected edit changed the saved binding: %+v %v", got, err)
				}
			}
		})
	}
}

func TestTaskExecutionResumesAgyAfterCompanionReconnects(t *testing.T) {
	f := newFakeHub().withMachine()
	shell := proto.Launcher{Name: "agy", Agent: "agy", Source: "zshrc"}
	runtime := proto.Launcher{Name: "agy", Agent: "agy", Source: "agy", Assistant: true}
	f.launcherList = []proto.Launcher{runtime}
	s := newTestService(t, f)
	task := Task{ID: "job", Prompt: "Check files", Repeat: repeatDaily, Time: "09:00", Enabled: true}
	setTasks(s, task)
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.agy"})

	now := time.Now()
	first, err := s.runTask(task, now, now, false, false)
	if err != nil || first.SessionID == "" {
		t.Fatalf("initial run: %+v %v", first, err)
	}
	waitRuns(t, s, 1)
	f.mu.Lock()
	f.launcherList = []proto.Launcher{shell}
	f.mu.Unlock()
	task, _ = taskByID(s, task.ID)
	_, err = s.runTask(task, now, now, false, false)
	if !strings.Contains(i18n.Localize(err, "en"), "Antigravity Assistant runtime is unavailable on server") || f.sent() != 1 {
		t.Fatalf("missing companion: err=%v sends=%d", err, f.sent())
	}
	failed := lastRunOf(t, s, task.ID)
	task, _ = taskByID(s, task.ID)
	if failed.Status != runFailed || failed.DeliveryUnknown || task.RunSessionID != first.SessionID || task.RunLauncher != "agy" {
		t.Fatalf("refusal changed the saved conversation or delivery status: run=%+v task=%+v", failed, task)
	}

	f.mu.Lock()
	f.launcherList = []proto.Launcher{runtime}
	f.mu.Unlock()
	resumed, err := s.runTask(task, now, now, true, false)
	p := f.lastSend(t)
	if err != nil || f.sent() != 2 || resumed.SessionID != first.SessionID || p["launcher"] != "agy" ||
		p["session_id"] != first.SessionID || p["resume_only"] != true {
		t.Fatalf("restored companion did not resume the saved conversation: %+v %v payload=%v", resumed, err, p)
	}
}

func TestTaskPickerDefaultDoesNotChangeAssistantPicker(t *testing.T) {
	fastLauncherRecovery(t)
	f := newFakeHub().withMachine()
	f.sessions["remote"] = nil
	f.zshrc = map[string][]proto.Launcher{
		"server": {{Name: "main", Source: "zshrc"}},
		"remote": {{Name: "remote", Source: "zshrc"}, {Name: "interactive", Agent: "dsh", Source: "dsh"}},
	}
	s := newTestService(t, f)
	// The last successful discovery on the source computer establishes the inherited launcher's agent kind.
	s.models.zshrc = map[string][]proto.Launcher{"server": {{Name: "main", Source: "zshrc"}}}
	s.models.zshrcSync = map[string]LauncherSync{}
	f.hello.Models.LauncherRecovery = map[string]LauncherRecovery{
		"remote": {WaitSeconds: 1, Fallbacks: map[string][]string{"main": {"remote"}}},
	}
	main := ModelBinding{Provider: "launcher.main", Model: "main-model", Effort: "high"}
	setModel(t, s, "assistant", main)
	for _, purpose := range []string{"assistant", "task", "assistant"} {
		r := request(s, "GET", "/api/v1/machines/remote/launchers?purpose="+purpose, "")
		var body struct {
			Default   struct{ Provider, Model, Effort string }
			Launchers []proto.Launcher
		}
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &body) != nil {
			t.Fatalf("%s picker: %d %s", purpose, r.Code, r.Body.String())
		}
		wantProvider, wantModel, wantEffort := "main", "main-model", "high"
		if purpose == "task" {
			wantProvider, wantModel, wantEffort = "remote", "", ""
		}
		if body.Default.Provider != wantProvider || body.Default.Model != wantModel || body.Default.Effort != wantEffort {
			t.Errorf("%s picker inherited the other purpose's binding: %+v", purpose, body.Default)
		}
		for _, l := range body.Launchers {
			if l.Name == "interactive" {
				t.Errorf("%s picker offered an incompatible launcher", purpose)
			}
		}
	}
	if got, err := s.resolveModel(context.Background(), "assistant"); err != nil || got != main {
		t.Fatalf("picker changed the assistant configuration: %+v %v", got, err)
	}
}
