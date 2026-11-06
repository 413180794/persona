package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// The finished push says what the turn came to even when its answer is a table or folded in <details> (#4462).
func TestDonePushOfATableOrFoldedAnswer(t *testing.T) {
	for in, want := range map[string]string{
		"| 项 | 状态 | |---|---| | 构建 | 通过 |":             "项 · 状态 · 构建 · 通过",
		"<details><summary>结果</summary>全部通过</details>": "结果 全部通过",
	} {
		if body := alertOf(t, donePayloadIn(i18n.ZhHans, "m1", "box", "s1", "构建", in))["body"]; body != want {
			t.Errorf("%q: body %q, want %q", in, body, want)
		}
	}
}
