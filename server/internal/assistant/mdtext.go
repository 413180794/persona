package assistant

import (
	"strings"
	"unicode/utf8"
)

// A copy of the hub's clipMarkdown (hub/mdtext.go): a receipt's summary is the agent's last reply cut for a client to
// render.

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
