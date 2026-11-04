package channel

import "strings"

// Split cuts long Markdown into pieces of at most n runes (no limit when n <= 0), at line breaks where it can and never inside a code
// block without closing and reopening it.
func Split(s string, n int) []string {
	if n <= 0 || len([]rune(s)) <= n {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curLen := 0
	fence := ""
	flush := func() {
		if curLen == 0 {
			return
		}
		piece := cur.String()
		if fence != "" {
			piece = strings.TrimRight(piece, "\n") + "\n```"
		}
		out = append(out, strings.TrimRight(piece, "\n"))
		cur.Reset()
		curLen = 0
		if fence != "" {
			cur.WriteString(fence + "\n")
			curLen = len([]rune(fence)) + 1
		}
	}
	room := n - 8 // what a closing fence takes
	for _, line := range strings.SplitAfter(s, "\n") {
		runes := []rune(line)
		if curLen+len(runes) > room {
			flush()
		}
		for curLen+len(runes) > room { // one line longer than a piece: cut it
			cut := room - curLen
			cur.WriteString(string(runes[:cut]))
			curLen += cut
			runes = runes[cut:]
			flush()
		}
		cur.WriteString(string(runes))
		curLen += len(runes)
		if t := strings.TrimSpace(string(runes)); strings.HasPrefix(t, "```") {
			if fence == "" {
				fence = string([]rune(t)[:min(len([]rune(t)), 20)]) // reopened on every piece: keep it short
			} else {
				fence = ""
			}
		}
	}
	if curLen > 0 {
		fence = ""
		out = append(out, strings.TrimRight(cur.String(), "\n"))
	}
	return out
}
