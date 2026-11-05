package textfmt

import (
	"regexp"
	"strings"

	"github.com/nwverse/persona/sdk/go/wire"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

var plainParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// What a clip can leave behind with nothing to pair with: emphasis and strike-through runs, backticks.
var danglingMarks = regexp.MustCompile("\\*{2,}|_{2,}|~{2,}|`+")

// PlainMarkdown is md without its markup, for places that show text as it is (a push, a one-line preview): a line per
// block, emphasis and code without their marks, a link as its text. Marks a clip left unpaired are dropped too.
func PlainMarkdown(md string) string { return plain(md, true) }

// plain is md as plain text; dropDangling also drops emphasis marks and backticks left over after the parse, which a
// clip leaves unpaired but which in an uncut text are its content (__init__.py, a**b).
func plain(md string, dropDangling bool) string {
	md = wire.AssistantCardText(md)
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
		if dropDangling {
			l = danglingMarks.ReplaceAllString(l, "")
		}
		if l = OneLine(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// PlanTitle is what a plan card is called where it shows in one line (a push, the live activity, a row in the Agent
// lists): its first line with text, without the Markdown marks (a heading's #s, emphasis, a list item's bullet or
// checkbox, code spans, a link's target) that would show as they are.
func PlanTitle(plan string) string {
	for _, l := range strings.Split(plan, "\n") {
		if l = plain(l, false); l != "" { // a plan is not clipped: what is left of its marks is its text
			return l
		}
	}
	return ""
}
