package hub

import "testing"

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
