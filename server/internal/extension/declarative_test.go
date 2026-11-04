package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclarativeContributions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "extension", "examples", "Manifest.example-declarative.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Skills()) != 1 || len(m.Prompts()) != 2 || len(m.Themes()) != 1 || len(m.TriageTemplates()) != 1 {
		t.Fatalf("%+v", m.Contributions())
	}
	parts := m.AssistantParts()
	if len(parts) != 2 || parts[0].Skills[0] != "skills/weekly-review" || parts[1].Instructions != "assistant/tone.md" {
		t.Fatalf("%+v", parts)
	}
	if got := ThemeProblems(m.Themes()[0]); len(got) != 0 {
		t.Fatal(got)
	}
	if got := ThemeProblems(ThemeItem{Light: map[string]string{"accent": "#FFEB3B"}, Dark: map[string]string{"accent": "#1A1A1A"}}); len(got) != 2 {
		t.Fatal("pale yellow on white, near-black on black:", got)
	}

	bad := map[string]string{
		"a theme role it may not give":  `"themes": [{"id": "x", "name": "x", "light": {"accent": "#000000", "background": "#FFFFFF"}, "dark": {"accent": "#FFFFFF"}}]`,
		"a color that is not #RRGGBB":   `"themes": [{"id": "x", "name": "x", "light": {"accent": "red"}, "dark": {"accent": "#FFFFFF"}}]`,
		"no accent":                     `"themes": [{"id": "x", "name": "x", "light": {"positive": "#000000"}, "dark": {"accent": "#FFFFFF"}}]`,
		"a quick prompt without text":   `"prompts": [{"id": "x", "kind": "quick", "title": "x"}]`,
		"instructions that are not .md": `"prompts": [{"id": "x", "kind": "instructions", "file": "a.txt"}]`,
		"another kind":                  `"prompts": [{"id": "x", "kind": "system", "file": "a.md"}]`,
		"a skill out of the package":    `"skills": [{"id": "x", "dir": "../x"}]`,
		"a template too long":           `"triage_templates": [{"id": "x", "title": "x", "text": "` + strings.Repeat("长", 4001) + `"}]`,
	}
	for name, c := range bad {
		doc := `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", "contributes": {` + c + `}}`
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestDeclarativeSkillIsInThePackage(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	dir := t.TempDir()
	doc := `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0",
		"contributes": {"skills": [{"id": "s", "dir": "skills/s"}], "prompts": [{"id": "p", "kind": "instructions", "file": "assistant/p.md"}]}}`
	_ = os.WriteFile(filepath.Join(dir, ManifestFile), []byte(doc), 0o644)
	if _, err := r.Preview(t.Context(), "dir:"+dir, ""); err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatal("its skill is not there:", err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "skills", "s"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "skills", "s", "SKILL.md"), []byte("# s\n"), 0o644)
	if _, err := r.Preview(t.Context(), "dir:"+dir, ""); err == nil || !strings.Contains(err.Error(), "assistant/p.md") {
		t.Fatal("its instructions are not there:", err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "assistant"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "assistant", "p.md"), []byte("Be brief.\n"), 0o644)
	if _, err := r.Preview(t.Context(), "dir:"+dir, ""); err != nil {
		t.Fatal(err)
	}
}
