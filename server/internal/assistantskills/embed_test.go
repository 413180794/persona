package assistantskills

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func skill(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quiet(string, ...any) {}

// The user's skills are linked in, follow their space, and a skill gone from it is unlinked; a directory the user put
// in the assistant's skills themselves is never touched.
func TestSyncLinksTheUsersSkills(t *testing.T) {
	user, dir := t.TempDir(), filepath.Join(t.TempDir(), ".claude", "skills")
	skill(t, user, "alpha", "first")
	skill(t, user, "beta", "second")
	if err := os.MkdirAll(filepath.Join(dir, "beta"), 0o755); err != nil { // the user's own, in place
		t.Fatal(err)
	}
	if err := Sync(dir, user, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "alpha")); err != nil || target != filepath.Join(user, "alpha") {
		t.Fatalf("alpha: %q %v", target, err)
	}
	if !Has(dir, "alpha") {
		t.Fatal("alpha is not usable through the link")
	}
	if st, err := os.Lstat(filepath.Join(dir, "beta")); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("beta, a directory of the user's own, was replaced: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(user, "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := Sync(dir, user, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("a link to a skill gone from the user's space stayed: %v", err)
	}
}

// What an older jarvisd copied in (lark-connector, with its stamp) moves to the user's space once and is linked back.
func TestSyncMovesTheOldCopy(t *testing.T) {
	user, dir := filepath.Join(t.TempDir(), "skills"), t.TempDir()
	skill(t, dir, "lark-connector", "Feishu through lark-connector")
	if err := os.WriteFile(filepath.Join(dir, "lark-connector", "scripts", "ol.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyStamp), []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var said []string
	if err := Sync(dir, user, nil, func(f string, a ...any) { said = append(said, f) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(user, "lark-connector", "scripts", "ol.sh")); err != nil {
		t.Fatalf("not moved: %v", err)
	}
	if st, err := os.Lstat(filepath.Join(dir, "lark-connector")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("not linked back: %v", err)
	}
	if _, err := os.Stat(OlScript(dir)); err != nil {
		t.Fatalf("ol.sh is not reachable where the node looks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyStamp)); !os.IsNotExist(err) {
		t.Fatal("the old stamp stayed: the move would be tried again")
	}
	if len(said) == 0 {
		t.Fatal("the move is not logged")
	}
}

// The user already has lark-connector in their space: the old copy goes, theirs is linked.
func TestSyncOldCopyGivesWayToTheUsers(t *testing.T) {
	user, dir := t.TempDir(), t.TempDir()
	skill(t, user, "lark-connector", "mine")
	skill(t, dir, "lark-connector", "old copy")
	if err := os.WriteFile(filepath.Join(dir, legacyStamp), []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Sync(dir, user, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(filepath.Join(dir, "lark-connector")); target != filepath.Join(user, "lark-connector") {
		t.Fatalf("lark-connector points at %q", target)
	}
}

// Without the stamp nothing moves: a directory the user put there is theirs.
func TestNoStampNoMove(t *testing.T) {
	user, dir := t.TempDir(), t.TempDir()
	skill(t, dir, "lark-connector", "placed by hand")
	if err := Sync(dir, user, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(user, "lark-connector")); !os.IsNotExist(err) {
		t.Fatal("moved a directory jarvisd never put there")
	}
}

// A built-in skill is one Persona may redistribute: anything else goes in the user's space.
func TestBuiltinsAreRedistributable(t *testing.T) {
	for _, n := range builtinNames() {
		if Redistributable[n] == "" {
			t.Errorf("built-in skill %s is not registered as redistributable (embed.go)", n)
		}
	}
	for _, n := range legacyBundled {
		for _, b := range builtinNames() {
			if b == n {
				t.Errorf("%s is copied from elsewhere: it belongs in the user's space, not in jarvisd", n)
			}
		}
	}
}

func TestListAndDescriptions(t *testing.T) {
	user := t.TempDir()
	skill(t, user, "alpha", `"quoted one"`)
	if err := os.MkdirAll(filepath.Join(user, "folded"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: folded\ndescription: >\n  first line\n  second line\nlicense: MIT\n---\n"
	if err := os.WriteFile(filepath.Join(user, "folded", "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(user, "no-skill-md"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := List(user, nil)
	// The user's, then the built-in ones that need no connector (persona-office).
	if len(got) != 3 || got[0] != (Skill{"alpha", "quoted one", "user"}) || got[1] != (Skill{"folded", "first line second line", "user"}) ||
		got[2].Name != "persona-office" || got[2].Source != "builtin" || !strings.HasPrefix(got[2].Description, "生成 Word") {
		t.Fatalf("%+v", got)
	}
}

// The telegram skill is in while Telegram is signed in on the hub and out once it is not; the user's own skill of the
// name wins.
func TestConnectorSkillFollowsItsConnector(t *testing.T) {
	user, dir := t.TempDir(), t.TempDir()
	if err := Sync(dir, user, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if Has(dir, "telegram") || slices.ContainsFunc(List(user, nil), func(s Skill) bool { return s.Name == "telegram" }) {
		t.Fatal("the telegram skill is in with Telegram not signed in")
	}
	if err := Sync(dir, user, []string{"telegram"}, quiet); err != nil {
		t.Fatal(err)
	}
	if !Has(dir, "telegram") {
		t.Fatal("Telegram signed in, no telegram skill")
	}
	listed := List(user, []string{"telegram"})
	if i := slices.IndexFunc(listed, func(s Skill) bool { return s.Name == "telegram" }); i < 0 || listed[i].Source != "builtin" ||
		!strings.Contains(listed[i].Description, "tg") {
		t.Fatalf("listed %+v", listed)
	}
	if err := Sync(dir, user, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "telegram")); !os.IsNotExist(err) {
		t.Fatalf("signed out, the telegram skill stayed: %v", err)
	}
	skill(t, user, "telegram", "mine")
	if err := Sync(dir, user, []string{"telegram"}, quiet); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(filepath.Join(dir, "telegram")); target != filepath.Join(user, "telegram") {
		t.Fatalf("the user's telegram skill gave way to the built-in one: %q", target)
	}
}

// The agents Persona starts get the ForAgents skills as a Claude Code plugin: a manifest, the skills with their
// scripts runnable; written again only when what the binary carries changed.
func TestWritePlugin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-plugin")
	if err := WritePlugin(dir); err != nil {
		t.Fatal(err)
	}
	var manifest struct{ Name, Version string }
	b, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(b, &manifest) != nil || manifest.Name != "persona" || manifest.Version != builtinVersion() {
		t.Fatalf("manifest %s %v", b, err)
	}
	for _, name := range ForAgents {
		if _, err := os.Stat(filepath.Join(dir, "skills", name, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "skills", "persona-office", "scripts", "office.sh")); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("the script is not runnable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "telegram")); !os.IsNotExist(err) {
		t.Fatal("an assistant-only skill is in the agents' plugin")
	}
	// The same binary: nothing written; another one: written again.
	marker := filepath.Join(dir, "skills", "persona-office", "marker")
	os.WriteFile(marker, nil, 0o644)
	if err := WritePlugin(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("written again with nothing changed")
	}
	os.WriteFile(filepath.Join(dir, builtinStamp), []byte("older\n"), 0o644)
	if err := WritePlugin(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("not written again for another binary")
	}
	if _, err := os.Stat(dir + ".old"); !os.IsNotExist(err) {
		t.Fatal("the old copy stayed")
	}
}

// The skill's script runs `personad office …` with the node's personad (PERSONA_BIN), or says there is none.
func TestOfficeScript(t *testing.T) {
	dir := t.TempDir()
	if err := copyBuiltin("persona-office", filepath.Join(dir, "skill")); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "personad")
	os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755)
	script := filepath.Join(dir, "skill", "scripts", "office.sh")
	cmd := exec.Command("sh", script, "docx", "周报.md", "-o", "周报.docx")
	cmd.Env = []string{"PERSONA_BIN=" + bin, "HOME=" + dir, "PATH=/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "office docx 周报.md -o 周报.docx" {
		t.Fatalf("%v %s", err, out)
	}
	cmd = exec.Command("sh", script, "check", "x.docx")
	cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || !strings.Contains(string(out), "no personad") {
		t.Fatalf("no personad: %v %s", err, out)
	}
}
