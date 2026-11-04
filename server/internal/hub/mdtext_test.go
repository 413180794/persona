package hub

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlainMarkdown(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"## 根因\nhub 和 node 的 **会话** 不同步", "根因\nhub 和 node 的 会话 不同步"},
		{"**网页的改动**\n- **底部的「收起」**：改成 `Collapse`\n- 第二条", "网页的改动\n底部的「收起」：改成 Collapse\n第二条"},
		{"调 `POST /api/v1/sessions/{mid}/{sid}/interrupt` 停下", "调 POST /api/v1/sessions/{mid}/{sid}/interrupt 停下"},
		{"看 [PR](https://github.com/o/r/pull/1) 和 <https://example.com>", "看 PR 和 https://example.com"},
		{"> 引用 ~~删掉~~ *斜体*\n\n```go\nx := 1\n```", "引用 删掉 斜体\nx := 1"},
		{"| a | b |\n| - | - |\n| 1 | 2 |", "a\nb\n1\n2"},
		{"剪在 **加粗中间…", "剪在 加粗中间…"},
		{"剪在 `代码中间…", "剪在 代码中间…"},
		{"完成了", "完成了"},
		{"", ""},
	} {
		if got := plainMarkdown(tc.in); got != tc.want {
			t.Errorf("plainMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClipMarkdown(t *testing.T) {
	short := "**完成**：改了 `a.go`"
	if got := clipMarkdown(short, 100); got != short {
		t.Errorf("a short text is kept: %q", got)
	}
	for _, tc := range []struct {
		name, in string
		n        int
		want     string
	}{
		{"on a line break", "## 根因\n- **一**：甲乙丙丁\n- **二**：戊己庚辛壬癸", 22, "## 根因\n- **一**：甲乙丙丁…"},
		{"not inside bold", "前面的说明文字 **很长的加粗内容在这里**", 15, "前面的说明文字…"},
		{"not inside code", "前面的说明文字 `POST /api/v1/x` 后面", 15, "前面的说明文字…"},
		{"not inside a link", "前面的说明文字 [链接文字](https://example.com/a)", 25, "前面的说明文字…"},
		{"a closed span stays", "改了 `a.go` 和 **b** 还有很多很多很多", 18, "改了 `a.go` 和 **b**…"},
		{"a code block is closed", "说明\n```go\nfunc a() {}\nfunc b() {}\nfunc c() {}", 34, "说明\n```go\nfunc a() {}\nfunc b() {}\n```\n…"},
	} {
		got := clipMarkdown(tc.in, tc.n)
		if got != tc.want {
			t.Errorf("%s: clipMarkdown = %q, want %q", tc.name, got, tc.want)
		}
		if strings.Count(got, "**")%2 != 0 || strings.Count(got, "`")%2 != 0 {
			t.Errorf("%s: unpaired marks in %q", tc.name, got)
		}
		if utf8.RuneCountInString(got) > tc.n+len("\n```\n…") {
			t.Errorf("%s: %q is longer than %d", tc.name, got, tc.n)
		}
	}
}
