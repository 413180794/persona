package prompts

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each prompt's output is pinned by the golden tests of the package that uses it (hub, kb, node: *_golden_test.go);
// these check the package's own rules.

// Every file ends with a newline (it is dropped), so adding one in an editor never changes a prompt.
func TestFilesEndWithANewline(t *testing.T) {
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".md") {
			t.Errorf("%s: only .md files belong here", path)
		}
		raw, _ := files.ReadFile(path)
		if !strings.HasSuffix(string(raw), "\n") {
			t.Errorf("%s does not end with a newline", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Every template is used: rendered from Go here, or included by another template.
func TestEveryTemplateIsUsed(t *testing.T) {
	var src strings.Builder
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")) {
			raw, _ := os.ReadFile(path)
			src.Write(raw)
		}
		return nil
	})
	names := Names()
	if len(names) < 40 {
		t.Fatalf("only %d templates parsed", len(names))
	}
	for _, n := range names {
		if !strings.Contains(src.String(), `"`+n+`"`) {
			t.Errorf("template %s is never rendered or included", n)
		}
	}
}

// A field the data lacks is an error, not an empty string in a prompt.
func TestMissingFieldPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("rendering with the wrong data should panic")
		}
	}()
	render("inbox/triage", struct{ Owner string }{"x"})
}

func TestSmallPrompts(t *testing.T) {
	for got, want := range map[string]string{
		RenderRefused("他说不行"):                  "（没有执行）他说不行",
		RenderLauncherCheck():                  "Reply with exactly one word: ok",
		RenderJSONOnly("system"):               "system\n\nReply with a single JSON object only, no prose, no code fences.",
		RenderTranslateMailInput("S", "T"):     "主题：S\n\nT",
		RenderCodexInstructions("snap", "mem"): "snap\n\nmem",
		RenderCodexInstructions("", "mem"):     "mem",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestLarkTranscriptKeepsTheNewestThatFit(t *testing.T) {
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	lines := []LarkLine{{At: at, Who: "张三", Text: "早"}, {At: at.Add(time.Minute), Who: "我", Mine: true, Text: "/agent 看下", Request: true}}
	if got, want := RenderLarkTranscript(lines, 1000), "10-31 09:00 张三：早\n10-31 09:01 我（我）：/agent 看下  ← 请求\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := RenderLarkTranscript(lines, 60); strings.Contains(got, "张三") || !strings.Contains(got, "← 请求") {
		t.Fatalf("a short limit keeps the newest: %q", got)
	}
	if got := RenderLarkTranscript(nil, 50); got != "\n" {
		t.Fatalf("no lines: %q", got)
	}
}
