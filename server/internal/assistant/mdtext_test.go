package assistant

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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
