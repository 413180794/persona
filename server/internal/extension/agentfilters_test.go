package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const filterManifest = `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0",
	"configuration": {"type": "object", "properties": {"hide": {"type": "boolean", "title": "隐藏", "default": true}, "name": {"type": "string", "title": "名字"}}},
	"contributes": {"agent_filters": [{"id": "runs", "when": "hide", "agents": ["codex"], "originators": ["codex-bot"]},
		{"id": "always", "first_message_prefixes": ["You are a bot of the platform"]}]}}`

// An agent filter needs a matcher, a when that is a boolean field of the settings, absolute directories at least two
// levels deep and prefixes long enough not to take the user's own prompts.
func TestAgentFilterContributions(t *testing.T) {
	m, err := Parse([]byte(filterManifest))
	if err != nil {
		t.Fatal(err)
	}
	if fs := m.AgentFilters(); len(fs) != 2 || fs[0].When != "hide" || fs[0].Originators[0] != "codex-bot" {
		t.Fatalf("%+v", fs)
	}
	config := `"configuration": {"type": "object", "properties": {"hide": {"type": "boolean", "title": "隐藏"}, "name": {"type": "string", "title": "名字"}}},`
	bad := map[string]string{
		"no matcher":               `"agent_filters": [{"id": "x", "agents": ["codex"]}]`,
		"another agent":            `"agent_filters": [{"id": "x", "agents": ["gemini"], "originators": ["a"]}]`,
		"a short prefix":           `"agent_filters": [{"id": "x", "first_message_prefixes": ["hi"]}]`,
		"a relative directory":     `"agent_filters": [{"id": "x", "added_dirs": [".bot/bundles"]}]`,
		"the home directory":       `"agent_filters": [{"id": "x", "added_dirs": ["~/"]}]`,
		"a when that is not there": `"agent_filters": [{"id": "x", "when": "missing", "originators": ["a"]}]`,
		"a when that is a string":  `"agent_filters": [{"id": "x", "when": "name", "originators": ["a"]}]`,
		"a field it does not know": `"agent_filters": [{"id": "x", "originators": ["a"], "cwd": "/tmp"}]`,
	}
	for name, c := range bad {
		doc := `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", ` + config + ` "contributes": {` + c + `}}`
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// A filter with when holds while that field is true; without, always. Its switch is that field: a contribution
// switch left in the record does not take it out.
func TestAgentFilterSwitch(t *testing.T) {
	m, err := Parse([]byte(filterManifest))
	if err != nil {
		t.Fatal(err)
	}
	f, always := m.AgentFilters()[0], m.AgentFilters()[1]
	for raw, want := range map[string]bool{`{"hide": true}`: true, `{"hide": false}`: false, `{}`: false, `{"hide": "yes"}`: false} {
		if f.Holds(json.RawMessage(raw)) != want {
			t.Errorf("%s: want %v", raw, want)
		}
	}
	if !always.Holds(json.RawMessage(`{}`)) {
		t.Error("without when it always holds")
	}
	if field, ok := m.SwitchedByConfig("agent_filters", "runs"); !ok || field != "hide" {
		t.Fatal(field, ok)
	}
	if _, ok := m.SwitchedByConfig("agent_filters", "always"); ok {
		t.Fatal("without when it has the contribution switch")
	}
	kept := m.Without([]string{"agent_filters/runs", "agent_filters/always"}).AgentFilters()
	if len(kept) != 1 || kept[0].ID != "runs" {
		t.Fatalf("only the one without when is switched off: %+v", kept)
	}
}

func TestAgentFilterSwitchIsTheSetting(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(filterManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	pv, err := r.Preview(t.Context(), "dir:"+dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Install(t.Context(), pv.ID, pv.Digest, "cli"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetContribution(t.Context(), "kit", "agent_filters", "runs", false, "test"); err != ErrSwitchedByConfig {
		t.Fatal("switched in the settings:", err)
	}
	if err := r.SetContribution(t.Context(), "kit", "agent_filters", "always", false, "test"); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("kit").Manifest.AgentFilters(); !slices.ContainsFunc(got, func(f AgentFilterItem) bool { return f.ID == "runs" }) || len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}
