package hub

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

var plainParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// What a clip can leave behind with nothing to pair with: emphasis and strike-through runs, backticks.
var danglingMarks = regexp.MustCompile("\\*{2,}|_{2,}|~{2,}|`+")

// plainMarkdown is md without its markup, for places that show text as it is (a push, a one-line preview): a line per
// block, emphasis and code without their marks, a link as its text. Marks a clip left unpaired are dropped too.
func plainMarkdown(md string) string {
	src := []byte(md)
	var b strings.Builder
	_ = ast.Walk(plainParser.Parse(text.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n := n.(type) {
		case *ast.Text:
			if entering {
				b.Write(n.Segment.Value(src))
				if n.SoftLineBreak() || n.HardLineBreak() {
					b.WriteByte(' ')
				}
			}
		case *ast.String:
			if entering {
				b.Write(n.Value)
			}
		case *ast.AutoLink:
			if entering {
				b.Write(n.URL(src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering {
				lines := n.Lines()
				for i := 0; i < lines.Len(); i++ {
					seg := lines.At(i)
					b.Write(seg.Value(src))
				}
			}
		case *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkSkipChildren, nil
		}
		if !entering && n.Type() == ast.TypeBlock {
			b.WriteByte('\n')
		}
		return ast.WalkContinue, nil
	})
	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = oneLine(danglingMarks.ReplaceAllString(l, "")); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// clipMarkdown is md cut to at most about n characters for a client to render: on a line break when one falls in the
// second half, and never inside a code span, emphasis, link or code block (an unpaired ** or ` would show as it is).
func clipMarkdown(md string, n int) string {
	if utf8.RuneCountInString(md) <= n {
		return md
	}
	cut := string([]rune(md)[:n])
	midLine := true
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 && utf8.RuneCountInString(cut[:i]) >= n/2 {
		cut, midLine = strings.TrimRight(cut[:i], " \t\n"), false
	}
	lines := strings.Split(cut, "\n")
	if fence := openFence(lines); fence != "" {
		return cut + "\n" + fence + "\n…"
	}
	if midLine {
		lines[len(lines)-1] = strings.TrimRight(closeInline(lines[len(lines)-1]), " \t")
	}
	return strings.Join(lines, "\n") + "…"
}

// closeInline is a line cut in the middle without the code span, emphasis or link it was cut inside.
func closeInline(line string) string {
	if strings.Count(line, "`")%2 == 1 {
		line = line[:strings.LastIndexByte(line, '`')]
	}
	for _, mark := range []string{"**", "__", "~~"} {
		if strings.Count(line, mark)%2 == 1 {
			line = line[:strings.LastIndex(line, mark)]
		}
	}
	if i := strings.LastIndexByte(line, '['); i >= 0 {
		rest := line[i:]
		if j := strings.Index(rest, "]("); !strings.Contains(rest, "]") || j >= 0 && !strings.Contains(rest[j:], ")") {
			line = line[:i]
		}
	}
	return line
}

// openFence is the ``` or ~~~ of a code block the lines leave open, if any.
func openFence(lines []string) string {
	open := ""
	for _, l := range lines {
		f := leadingFence(strings.TrimSpace(strings.TrimLeft(l, " \t>")))
		switch {
		case f == "":
		case open == "":
			open = f
		case f[0] == open[0] && len(f) >= len(open) && strings.TrimSpace(strings.TrimLeft(l, " \t>")) == f:
			open = ""
		}
	}
	return open
}

// leadingFence is the ``` or ~~~ run a line starts with, if any.
func leadingFence(line string) string {
	for _, c := range []byte{'`', '~'} {
		i := 0
		for i < len(line) && line[i] == c {
			i++
		}
		if i >= 3 {
			return line[:i]
		}
	}
	return ""
}
