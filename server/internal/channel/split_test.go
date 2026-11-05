package channel

import (
	"strings"
	"testing"
)

// assertFencesBalanced replays Split's fence rule over one piece: every piece must leave every block closed.
func assertFencesBalanced(t *testing.T, p string) {
	open := ""
	for _, l := range strings.Split(p, "\n") {
		if m, _, bare := fenceLine(l); m != "" {
			if open == "" {
				open = m
			} else if m[0] == open[0] && len(m) >= len(open) && bare {
				open = ""
			}
		}
	}
	if open != "" {
		t.Fatalf("piece leaves a %q code block open:\n%s", open, p)
	}
}
func bareFence(l string) bool { m, _, bare := fenceLine(l); return m != "" && bare }

// reassemble removes the close/reopen fence pairs Split inserts at piece seams and joins the source back.
func reassemble(parts []string) string {
	lines := make([][]string, len(parts))
	for i, p := range parts {
		lines[i] = strings.Split(p, "\n")
	}
	for i := 0; i+1 < len(lines); i++ {
		a, b := lines[i], lines[i+1]
		if len(a) > 0 && len(b) > 0 && bareFence(a[len(a)-1]) {
			if m, _, _ := fenceLine(b[0]); m != "" {
				lines[i], lines[i+1] = a[:len(a)-1], b[1:] // injected close plus reopen
			}
		}
	}
	cores := make([]string, len(lines))
	for i, l := range lines {
		cores[i] = strings.Join(l, "\n")
	}
	return strings.Join(cores, "\n")
}
func TestSplitFencesAcrossPieces(t *testing.T) {
	cases := []struct {
		name, text, marker, head string
		n                        int
		noFence, crlf, midCut    bool
	}{
		{"tilde", "开头\n~~~\n" + strings.Repeat("echo hi\n", 40) + "~~~\n结尾", "~~~", "~~~", 60, false, false, false},
		{"backtick with info string", "开头\n```go\n" + strings.Repeat("fmt.Println(1)\n", 40) + "```\n结尾", "```", "```go", 200, false, false, false},
		{"four backticks with inner triple fence", "说明\n````\n" + strings.Repeat("code line\n", 12) + "```go\nx := 1\n```\n" + strings.Repeat("more code\n", 12) + "````\n尾部", "````", "````", 80, false, false, false},
		{"tilde block ignores inner backticks", "开头\n~~~\n" + strings.Repeat("a\n", 20) + "```not close\n" + strings.Repeat("b\n", 20) + "~~~\n结尾", "~~~", "~~~", 60, false, false, false},
		{"inner longer run with text does not close", "~~~~\n" + strings.Repeat("~", 47) + "x\n" + strings.Repeat("c\n", 40) + "~~~~\nend", "~~~~", "~~~~", 60, false, false, false},
		{"inner long run with info string does not close", "```\n" + strings.Repeat("`", 20) + "python xyz\n" + strings.Repeat("c\n", 40) + "```\nend", "```", "```", 60, false, false, false},
		{"inner run padded with spaces then text does not close", "```\n```" + strings.Repeat(" ", 17) + "x\n" + strings.Repeat("c\n", 40) + "```\nend", "```", "```", 60, false, false, false},
		{"fence-looking line with text does not close", "```\n" + strings.Repeat("a\n", 10) + "``` not close\n" + strings.Repeat("b\n", 30) + "```\nend", "```", "```", 60, false, false, false},
		{"closing fence longer than the opening one", "``````\n" + strings.Repeat("x\n", 40) + "```````\nend", "``````", "``````", 80, false, false, false},
		{"up to three leading spaces still make a fence", "x\n   ```\n" + strings.Repeat("line\n", 30) + "   ```\nend", "```", "```", 60, false, false, false},
		{"CRLF line endings", "开头\r\n~~~\r\n" + strings.Repeat("echo hi\r\n", 40) + "~~~\r\n结尾", "~~~", "~~~", 60, false, true, false},
		{"hard-cut long line inside a backtick fence", "开头\n" + "```" + "\n" + strings.Repeat("字", 200) + "\n```\n结尾", "```", "```", 60, false, false, true},
		{"hard-cut long line inside a tilde fence", "开头\n~~~\n" + strings.Repeat("字", 200) + "\n~~~\n结尾", "~~~", "~~~", 60, false, false, true},
		{"four-space indent is not a fence", strings.Repeat("    ```not fence\n", 30) + "结尾", "", "", 60, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts := Split(tc.text, tc.n)
			if len(parts) < 2 {
				t.Fatalf("not split into pieces: %d", len(parts))
			}
			seams := 0
			for i, p := range parts {
				if n := len([]rune(p)); n > tc.n {
					t.Fatalf("piece %d has %d runes, limit %d", i, n, tc.n)
				}
				assertFencesBalanced(t, p)
				if tc.noFence {
					for _, l := range strings.Split(p, "\n") {
						if m, _, _ := fenceLine(l); m != "" {
							t.Fatalf("piece %d treats an indented line as a fence: %q", i, l)
						}
					}
				}
			}
			for i := 0; i+1 < len(parts) && !tc.noFence; i++ {
				a := strings.Split(parts[i], "\n")
				if c := a[len(a)-1]; bareFence(c) && strings.Split(parts[i+1], "\n")[0] == tc.head {
					if c != tc.marker {
						t.Fatalf("seam after piece %d closes with %q, want %q", i, c, tc.marker)
					}
					seams++
				}
			}
			if !tc.noFence && seams == 0 {
				t.Fatal("the block never crossed a piece boundary; no close/reopen seam was exercised")
			}
			want, got := tc.text, reassemble(parts)
			if tc.crlf { // a seam inside CRLF text joins on an injected LF
				want, got = strings.ReplaceAll(want, "\r\n", "\n"), strings.ReplaceAll(got, "\r\n", "\n")
			} else if tc.midCut { // a mid-line hard cut injects newlines; compare with every newline dropped
				want, got = strings.ReplaceAll(want, "\n", ""), strings.ReplaceAll(got, "\n", "")
			}
			if got != want {
				t.Fatalf("reassembled text differs\n got: %q\nwant: %q", got, want)
			}
		})
	}
}
func TestSplitLongFenceOpenerFitsPiece(t *testing.T) {
	// The opening piece must never overflow on a long fence (n+L-8 at L >= 9 regression): prose flushes first.
	for _, tc := range []struct{ prose, n int }{{40, 60}, {3490, 3500}} {
		for _, L := range []int{9, 10, 12, 20, 50} {
			if L > tc.n/2 {
				continue
			}
			mark := strings.Repeat("`", L)
			text := strings.Repeat("字", tc.prose) + "\n" + mark + "\n" + strings.Repeat("x\n", 30) + mark + "\nend"
			parts := Split(text, tc.n)
			for i, p := range parts {
				if n := len([]rune(p)); n > tc.n {
					t.Fatalf("prose=%d n=%d L=%d piece %d has %d runes", tc.prose, tc.n, L, i, n)
				}
				assertFencesBalanced(t, p)
			}
			if got := reassemble(parts); got != text {
				t.Fatalf("prose=%d L=%d reassembled text differs", tc.prose, L)
			}
		}
	}
}
func TestSplitSmallNDoesNotPanicOrHang(t *testing.T) {
	// splitOK enforces the strict bound: degraded pieces stay at n too (stale marker must not inflate them).
	splitOK := func(src string, n int) ([]string, string) {
		parts := Split(src, n)
		joined := ""
		for i, p := range parts {
			joined += p
			if p == "" || len([]rune(p)) > n {
				t.Fatalf("n=%d piece %d invalid: %q", n, i, p)
			}
		}
		return parts, joined
	}
	for _, n := range []int{1, 8, 12, 20, 29, 30} {
		_, joined := splitOK("```go\n"+strings.Repeat("x", 200)+"\n```\nend", n)
		if !strings.Contains(joined, "end") || strings.Count(joined, "x") != 200 {
			t.Fatalf("n=%d lost content", n)
		}
	}
	// A closer longer than its opener cannot be balanced at small n: degrade first, but emit it intact.
	parts, joined := splitOK("```\n"+strings.Repeat("y", 60)+"\n``````\nend", 9)
	intact := false
	for _, p := range parts {
		intact = intact || p == "``````" // the long closer was never hard-cut
	}
	if !intact || !strings.Contains(joined, "end") || strings.Count(joined, "y") != 60 {
		t.Fatalf("closer-degrade lost content: %q", joined)
	}
	// A mid-line degrade must not let the current line's marker reopen the fence (stale-marker regression).
	stale := "`````````\n" + strings.Repeat("~", 47) + "info\n" + strings.Repeat("z", 120) + "\n`````````\nend"
	_, joined = splitOK(stale, 20)
	if strings.Count(joined, "~") != 47 || strings.Count(joined, "z") != 120 || !strings.Contains(joined, "end") {
		t.Fatalf("stale-marker degrade lost content: %q", joined)
	}
	if got := Split(strings.Repeat("\n", 100), 10); len(got) != 1 || got[0] != strings.Repeat("\n", 100) {
		t.Fatalf("whitespace-only input: %q", got)
	}
}
func TestFenceLine(t *testing.T) {
	cases := []struct {
		line, marker, head string
		bare               bool
	}{
		{"```", "```", "```", true},
		{"```go", "```", "```go", false},
		{"~~~text", "~~~", "~~~text", false},
		{"   ```", "```", "```", true},
		{"    ```", "", "", false},         // four spaces: indented code block
		{"\t```", "", "", false},           // a tab indents past column 3
		{"``", "", "", false},              // too short
		{"a```", "", "", false},            // not at the start
		{"```\t", "```", "```", false},     // only a tab after the run: not a closing fence
		{"```a`b", "```", "```a`b", false}, // backticks in the info string stay part of the line
		{"~~~a~b", "~~~", "~~~a~b", false},
		{"```" + strings.Repeat("语", 25), "```", "```" + strings.Repeat("语", 25), false}, // full info, capped only on reopen
	}
	for _, tc := range cases {
		if m, h, b := fenceLine(tc.line); m != tc.marker || h != tc.head || b != tc.bare {
			t.Fatalf("fenceLine(%q) = %q, %q, %v; want %q, %q, %v", tc.line, m, h, b, tc.marker, tc.head, tc.bare)
		}
	}
}
