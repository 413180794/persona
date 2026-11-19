package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A start's speed (launcher_speed.go): agent_start, task_plan and an Agent preset carry it to the node's session.start;
// one the Agent that runs cannot take is refused before anything starts, saying why.

func TestAgentStartSpeed(t *testing.T) {
	run := func(t *testing.T, tier ModelTier, input map[string]any) (*fakeHub, error) {
		t.Helper()
		s, f := newQuietServiceOn(t, &memStore{})
		f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: "codex", Agent: "codex"},
			{Name: "agy", Agent: "agy"}, {Name: "traex", Agent: "traex"}}
		if tier.Launcher != "" {
			saveTier(t, s, "quick", tier)
		}
		input["machine"], input["cwd"], input["prompt"] = "box", "/code/app", "Fix the failure"
		args, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.runTool(context.Background(), "server", "conv1", "agent_start", args)
		return f, err
	}
	for _, tc := range []struct {
		name  string
		tier  ModelTier
		input map[string]any
		want  string // the start's speed; "-" none sent
		err   string // a refusal saying this
	}{
		{name: "codex fast", input: map[string]any{"launcher": "codex", "speed": "fast"}, want: "fast"},
		{name: "codex ultrafast", input: map[string]any{"launcher": "codex", "speed": "ultrafast"}, want: "ultrafast"},
		{name: "claude fast", input: map[string]any{"launcher": "claude", "speed": "fast"}, want: "fast"},
		{name: "claude ultrafast", input: map[string]any{"launcher": "claude", "speed": "ultrafast"}, err: "runs claude, which cannot start at speed ultrafast"},
		{name: "agy fast", input: map[string]any{"launcher": "agy", "speed": "fast"}, err: "runs agy, which cannot start at speed fast"},
		{name: "traex fast", input: map[string]any{"launcher": "traex", "speed": "fast"}, err: "runs traex, which cannot start at speed fast"},
		{name: "agy standard", input: map[string]any{"launcher": "agy", "speed": "standard"}, want: "standard"},
		{name: "unknown", input: map[string]any{"launcher": "codex", "speed": "turbo"}, err: `unknown speed "turbo"`},
		{name: "none", input: map[string]any{"launcher": "codex"}, want: "-"},
		{name: "tier", tier: ModelTier{Launcher: "codex", Model: "gpt-test", Speed: "fast"}, input: map[string]any{"tier": "quick"}, want: "fast"},
		{name: "tier overridden", tier: ModelTier{Launcher: "codex", Speed: "fast"}, input: map[string]any{"tier": "quick", "speed": "standard"}, want: "standard"},
		{name: "tier, another model", tier: ModelTier{Launcher: "codex", Model: "gpt-test", Speed: "ultrafast"},
			input: map[string]any{"tier": "quick", "model": "gpt-other"}, want: "-"},
		{name: "tier, another launcher", tier: ModelTier{Launcher: "codex", Speed: "ultrafast"},
			input: map[string]any{"tier": "quick", "launcher": "claude"}, want: "-"},
		{name: "agy tier", tier: ModelTier{Launcher: "agy", Speed: "fast"}, input: map[string]any{"tier": "quick"}, err: "runs agy, which cannot start at speed fast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := run(t, tc.tier, tc.input)
			f.mu.Lock()
			defer f.mu.Unlock()
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) || !strings.Contains(err.Error(), "no agent was started") {
					t.Fatalf("error = %v, want one saying %q", err, tc.err)
				}
				if len(f.starts) != 0 {
					t.Fatalf("a refused start went on: %v", f.starts)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(f.starts) != 1 {
				t.Fatalf("starts = %v", f.starts)
			}
			got, sent := f.starts[0]["speed"].(string)
			if tc.want == "-" && sent || tc.want != "-" && got != tc.want {
				t.Fatalf("start speed = %q (sent %v), want %q: %v", got, sent, tc.want, f.starts[0])
			}
		})
	}
}

// task_plan: a speed the planned launcher cannot take is refused with the plan; at Start a tier's speed goes to the
// node with the task's own launch, and the task keeps the speed it ran with.
func TestTaskSpeed(t *testing.T) {
	t.Run("refused at plan", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, f := readonlyTaskService(t)
			f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: "agy", Agent: "agy"}}
			task := readonlyTestTask("")
			task.Readonly, task.Launcher, task.Speed = false, "agy", "fast"
			task.DoneWhen, task.ExpectMinutes = []string{"the login flow is fixed"}, 30
			_, err := s.toolTaskPlan(context.Background(), "chat", readonlyTaskArgs(t, task))
			synctest.Wait()
			if err == nil || !strings.Contains(err.Error(), "task 1: launcher agy runs agy, which cannot start at speed fast") {
				t.Fatalf("error = %v", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.starts) != 0 {
				t.Fatalf("a refused task started: %v", f.starts)
			}
		})
	})
	t.Run("unknown at plan", func(t *testing.T) {
		s, _ := readonlyTaskService(t)
		task := readonlyTestTask("")
		task.Speed = "warp"
		if _, err := s.toolTaskPlan(context.Background(), "chat", readonlyTaskArgs(t, task)); err == nil || !strings.Contains(err.Error(), `unknown speed "warp"`) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("tier at start", func(t *testing.T) {
		s, f := noModesService(t, "codex")
		saveTier(t, s, "quick", ModelTier{Launcher: "codex", Speed: "ultrafast"})
		task := readonlyTestTask("")
		task.Readonly, task.Tier = false, "quick"
		task.DoneWhen, task.ExpectMinutes = []string{"the login flow is fixed"}, 30
		if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
			t.Fatal(err)
		}
		if len(f.starts) != 1 || f.starts[0]["launcher"] != "codex" || f.starts[0]["speed"] != "ultrafast" {
			t.Fatalf("starts = %v", f.starts)
		}
		if task.Speed != "ultrafast" {
			t.Fatalf("task speed = %q", task.Speed)
		}
	})
	t.Run("the task's own over its tier", func(t *testing.T) {
		s, f := noModesService(t, "codex")
		saveTier(t, s, "quick", ModelTier{Launcher: "codex", Speed: "ultrafast"})
		task := readonlyTestTask("")
		task.Readonly, task.Tier, task.Speed = false, "quick", "fast"
		task.DoneWhen, task.ExpectMinutes = []string{"the login flow is fixed"}, 30
		if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
			t.Fatal(err)
		}
		if len(f.starts) != 1 || f.starts[0]["speed"] != "fast" {
			t.Fatalf("starts = %v", f.starts)
		}
	})
	t.Run("refused at start", func(t *testing.T) {
		s, f := noModesService(t, "cursor")
		saveTier(t, s, "quick", ModelTier{Launcher: "cursor", Speed: "fast"})
		task := readonlyTestTask("")
		task.Readonly, task.Tier = false, "quick"
		task.DoneWhen, task.ExpectMinutes = []string{"the login flow is fixed"}, 30
		if _, _, err := s.startTaskAgent(context.Background(), &task, false); err == nil || !strings.Contains(err.Error(), "runs cursor, which cannot start at speed fast") {
			t.Fatalf("error = %v", err)
		}
		if len(f.starts) != 0 {
			t.Fatalf("a refused task started: %v", f.starts)
		}
	})
}

// A card edit names a speed; one that names none keeps the assistant's.
func TestPlanEditSpeed(t *testing.T) {
	task := PlanTask{Speed: "fast"}
	applyPlanEdit(&task, PlanTask{Title: "renamed"})
	if task.Speed != "fast" {
		t.Fatalf("an edit without speed changed it: %q", task.Speed)
	}
	applyPlanEdit(&task, PlanTask{Speed: "standard"})
	if task.Speed != "standard" {
		t.Fatalf("speed = %q", task.Speed)
	}
}

// Agent presets keep a speed: checked when saved, set and dropped with the launcher by model_profiles.tiers, shown in
// the presets' line and the New Agent form's list.
func TestModelTierSpeed(t *testing.T) {
	if err := checkModelTiers(map[string]ModelTier{"quick": {Launcher: "codex", Speed: "warp"}}); err == nil || !strings.Contains(err.Error(), "warp") {
		t.Fatalf("an unknown speed saved: %v", err)
	}
	for _, speed := range append([]string{""}, proto.AgentSpeeds...) {
		if err := checkModelTiers(map[string]ModelTier{"quick": {Launcher: "codex", Speed: speed}}); err != nil {
			t.Fatalf("speed %q refused: %v", speed, err)
		}
	}

	ps := modelProfiles{Revision: 1, Profiles: []ModelProfile{{ID: "p", Name: "P", Tiers: map[string]ModelTier{"quick": {Launcher: "codex", Model: "gpt-test"}}}}}
	fast := "fast"
	if _, err := editTiers(&ps, tierEdit{Tier: "quick", Speed: &fast}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := ps.Profiles[0].Tiers["quick"]; got.Speed != "fast" || got.Model != "gpt-test" {
		t.Fatalf("tier = %+v", got)
	}
	if got := ps.Profiles[0].Tiers["quick"].runs(); got != "codex · gpt-test · fast" {
		t.Fatalf("runs = %q", got)
	}
	claude := "claude"
	if _, err := editTiers(&ps, tierEdit{Tier: "quick", Launcher: &claude}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := ps.Profiles[0].Tiers["quick"]; got.Speed != "" || got.Model != "" {
		t.Fatalf("another launcher kept the old one's speed or model: %+v", got)
	}

	s, f := newQuietServiceOn(t, &memStore{})
	f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: "codex", Agent: "codex"}}
	saveTier(t, s, "quick", ModelTier{Launcher: "codex", Speed: "ultrafast", Description: "urgent fixes"})
	if got := s.modelProfileContext(context.Background()); got == nil || len(got.Tiers) != 1 || got.Tiers[0].Speed != "ultrafast" {
		t.Fatalf("snapshot tiers = %+v", got)
	}
	text, err := s.getModelTiers(context.Background())
	if err != nil || !strings.Contains(text, "quick = codex · ultrafast") || !strings.Contains(text, "Speeds: standard, fast, ultrafast") {
		t.Fatalf("model_profiles.tiers = %q, %v", text, err)
	}
	launchers := []ConfiguredLauncher{{Launcher: proto.Launcher{Name: "codex", Agent: "codex"}}}
	if _, tiers := s.pickerTiers(context.Background(), "box", launchers); len(tiers) != 1 || tiers[0].Speed != "ultrafast" {
		t.Fatalf("picker tiers = %+v", tiers)
	}
	raw, _ := json.Marshal(pickerTier{ModelTier: ModelTier{Launcher: "codex", Speed: "fast"}, Name: "quick"})
	if !strings.Contains(string(raw), `"speed":"fast"`) {
		t.Fatalf("picker tier JSON = %s", raw)
	}
}
