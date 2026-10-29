package screen

import (
	"slices"
	"strings"
	"testing"
)

const askScreen = ` Ran 1 shell command
────────────────────────────────────────────────
 ☐ 发布方式

 │第一次构建失败：镜像里缺一个依赖，补上以后
 │编译通过，修复也已推送（a1b2c3d）。

❯ 1. 取消这次发布 (Recommended)
     用修复后的 commit 重新发一次，
     旧的那次先撤掉。
  2. 先不取消
     我停在这里，什么都不动。
  3. Type something.
────────────────────────────────────────────────
  4. Chat about this

 Enter to select · ↑/↓ to navigate · Esc to cancel`

func TestParseAskUserQuestion(t *testing.T) {
	d := ParseDialog(askScreen)
	if d == nil {
		t.Fatal("no dialog")
	}
	if d.Header != "发布方式" || d.Question != "第一次构建失败：镜像里缺一个依赖，补上以后编译通过，修复也已推送（a1b2c3d）。" {
		t.Fatalf("header %q question %q", d.Header, d.Question)
	}
	keys := []string{}
	for _, o := range d.Options {
		keys = append(keys, o.Key)
	}
	if !slices.Equal(keys, []string{"1", "2", "3", "4"}) || d.Options[0].Label != "取消这次发布 (Recommended)" {
		t.Fatalf("options %+v", d.Options)
	}
	if d.Options[0].Detail != "用修复后的 commit 重新发一次，旧的那次先撤掉。" || d.Options[1].Detail != "我停在这里，什么都不动。" {
		t.Fatalf("details %q / %q", d.Options[0].Detail, d.Options[1].Detail)
	}
	if d.Options[2].Kind != KindTypeSomething || d.Options[2].Detail != "" || d.Options[3].Kind != KindChat {
		t.Fatalf("kinds %+v", d.Options)
	}
	if !slices.Equal(d.Options[1].Keys, []string{"2"}) || len(d.Rev) != 12 {
		t.Fatalf("keys %v rev %q", d.Options[1].Keys, d.Rev)
	}
}

const permissionScreen = `────────────────────────────────────────────────
 Bash command

   git push origin main
   Push the release commit

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for git push commands in /code/app
   3. No, and tell Claude what to do differently (esc)`

func TestParsePermissionPrompt(t *testing.T) {
	d := ParseDialog(permissionScreen)
	if d == nil || d.Header != "" || d.Question != "Do you want to proceed?" || !strings.Contains(d.Detail, "git push origin main") {
		t.Fatalf("%+v", d)
	}
	if len(d.Options) != 3 || d.Options[0].Detail != "" {
		t.Fatalf("%+v", d.Options)
	}
}

// The revision follows what is asked, not where the cursor is or what moves around it.
func TestRevision(t *testing.T) {
	a := ParseDialog(permissionScreen)
	moved := strings.Replace(strings.Replace(permissionScreen, " ❯ 1. Yes", "   1. Yes", 1), "   2. Yes,", " ❯ 2. Yes,", 1)
	if b := ParseDialog("✻ Working… (3s)\n" + moved); b.Rev != a.Rev {
		t.Fatal("cursor or spinner changed the revision")
	}
	other := strings.Replace(permissionScreen, "git push origin main", "git push --force origin main", 1)
	if ParseDialog(other).Rev == a.Rev {
		t.Fatal("another command, same revision")
	}
}

func TestParseFixtures(t *testing.T) {
	trust := ParseDialog(Plain(fixture(t, "claude-2.1.312-trust-162.txt")))
	if trust == nil || len(trust.Options) != 2 || trust.Options[0].Label != "No, exit" || trust.Options[1].Label != "Yes, I trust this folder" {
		t.Fatalf("trust %+v", trust)
	}
	if !strings.HasPrefix(trust.Question, "Quick safety check: Is this a project you created or one you trust?") {
		t.Fatalf("trust question %q", trust.Question)
	}
	if !slices.Equal(trust.Options[0].Keys, []string{"enter"}) || !slices.Equal(trust.Options[1].Keys, []string{"down", "enter"}) {
		t.Fatalf("trust keys %v %v", trust.Options[0].Keys, trust.Options[1].Keys)
	}
	picker := ParseDialog(fixture(t, "claude-2.1.312-model-picker-162.txt"))
	if picker == nil || len(picker.Options) != 6 || !strings.HasPrefix(picker.Options[3].Label, "Sonnet") {
		t.Fatalf("picker %+v", picker)
	}
	rewind := ParseDialog(fixture(t, "claude-rewind-2.1.304-confirm-apple-restore-120.txt"))
	if rewind == nil || len(rewind.Options) != 4 || rewind.Options[3].Label != "Never mind" {
		t.Fatalf("rewind %+v", rewind)
	}
	for _, name := range []string{"claude-2.1.312-empty-162.txt", "claude-2.1.312-typed-162.txt"} {
		if d := ParseDialog(fixture(t, name)); d != nil {
			t.Fatalf("%s: %+v", name, d)
		}
	}
	if ParseDialog("> hello\n  some output") != nil {
		t.Fatal("no options, no dialog")
	}
}
