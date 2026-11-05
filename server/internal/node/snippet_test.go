package node

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

// benchLastText is an 18 KB last reply without a card, the common case pollAgents meets every second.
var benchLastText = func() string {
	line := "- Updated server/internal/node/sessions.go so the read-only check runs first; 已经确认 go test 通过。\n"
	return strings.Repeat(line, 18*1024/len(line)+1)
}()

func BenchmarkSnippet(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		snippet(benchLastText)
	}
}

// oldSnippet is snippet before the single pass; the rewrite must give the same bytes.
func oldSnippet(s string) string {
	s = wire.AssistantCardText(s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

func TestSnippetMatchesOld(t *testing.T) {
	runes := func(n int, piece string) string { return strings.Repeat(piece, n) }
	cases := []string{
		"", " ", " \t\r\n\v\f\u0085 　 ", "a", "  a  ", "a\r\nb\tc  d\n\n\ne",
		runes(200, "x"), runes(201, "x"), runes(199, "x") + " y", runes(200, "x") + " y", runes(199, "x") + "  \n",
		runes(200, "x") + "   ", " " + runes(201, "x"), runes(300, "界"), runes(200, "界"), runes(201, "界"),
		runes(100, "a ") + "b", runes(101, "a\r\n"), runes(150, "é\t"),
		"Conclusion\n```persona-card\n{\"title\":\"Build\",\"value\":3}\n```\n" + runes(250, "w "),
		"x\n```persona-view\n{\"fallback\":\"Done\",\"sections\":[]}\n```\nafter",
		"bad \xff\xfe utf8", runes(199, "x") + "\xff" + runes(5, "y"), runes(210, "\xc5"), "\xe7\x95 half",
		benchLastText,
	}
	rng := rand.New(rand.NewPCG(3, 4))
	pieces := []string{"a", "界", " ", "\n", "\r\n", "\t", " ", " ", "\xff", "é", "- ", "persona-card"}
	for range 3000 {
		var b strings.Builder
		for range rng.IntN(260) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		cases = append(cases, b.String())
	}
	for _, s := range cases {
		if got, want := snippet(s), oldSnippet(s); got != want {
			t.Fatalf("snippet(%q)\n got %q\nwant %q", s, got, want)
		}
	}
}
