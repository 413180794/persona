package channel

import "strings"

// Split cuts long Markdown into pieces of at most n runes (no limit when n <= 0), at line breaks where it can
// and never inside a code block without closing and reopening it. Whitespace-only input is returned as one piece.
func Split(s string, n int) []string {
	if n <= 0 || len([]rune(s)) <= n {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curLen := 0
	fenceMark := "" // opening fence run, e.g. "~~~" or "````"
	fenceHead := "" // trimmed opening line, used to reopen the block on the next piece
	degraded := false
	room := n - 8 // leave room for closing the piece's fence
	flush := func() {
		if curLen == 0 {
			return
		}
		piece := cur.String()
		if fenceMark != "" {
			piece = strings.TrimRight(piece, "\n") + "\n" + fenceMark
		}
		if text := strings.TrimRight(piece, "\n"); text != "" {
			out = append(out, text)
		}
		cur.Reset()
		curLen = 0
		if fenceMark != "" {
			cur.WriteString(fenceHead + "\n")
			curLen = len([]rune(fenceHead)) + 1
		}
	}
	// degrade stops balancing fences and hard-cuts the rest as plain text. flush closes the current piece,
	// then its reopen prefix is discarded; reachable only for tiny n, and every piece stays within n.
	degrade := func() {
		flush()
		cur.Reset() // discard the reopen prefix flush wrote: no fence follows after degradation
		curLen = 0
		fenceMark, fenceHead, degraded = "", "", true
		room = max(n, 1)
	}
	for _, line := range strings.SplitAfter(s, "\n") {
		runes := []rune(line)
		m, head, bare := "", "", false
		if !degraded {
			m, head, bare = fenceLine(strings.TrimRight(line, "\n"))
		}
		opens := fenceMark == "" && m != ""
		closes := fenceMark != "" && m != "" && m[0] == fenceMark[0] && len(m) >= len(fenceMark) && bare
		if opens {
			if curLen > 0 && curLen+len(runes)+len(m)+1 > n {
				flush() // prose before the opener starts its own piece, which must have room for a close
			}
			if len(runes)+len(m)+1 > n { // opener plus an injected close cannot fit one piece: give up
				degrade()
				opens = false
			}
		} else if closes {
			if len([]rune(fenceHead))+1+len(runes) > n { // reopen plus whole closer cannot fit: never hard-cut a closer
				degrade()
				closes = false
			} else if curLen+len(runes) > n {
				flush() // next piece starts with the reopen, then carries the whole closer
			}
		}
		if !opens && !closes {
			if curLen+len(runes) > room {
				flush()
			}
			for curLen+len(runes) > room { // one line longer than a piece: cut it
				cut := room - curLen
				if cut <= 0 {
					degrade()
					continue
				}
				cur.WriteString(string(runes[:cut]))
				curLen += cut
				runes = runes[cut:]
				flush()
			}
		}
		cur.WriteString(string(runes))
		curLen += len(runes)
		if m != "" && !degraded { // degrade on this very line invalidates its own marker
			switch {
			case fenceMark == "":
				fenceMark = m
				fenceHead = head // cap the reopen copy so a long info string cannot eat the next piece's budget
				if r := []rune(head); len(r) > max(len(m), 20) {
					fenceHead = string(r[:max(len(m), 20)])
				}
				room = n - len(m) - 1 // bound the piece so its injected close fits
			case closes:
				fenceMark, fenceHead = "", "" // same char, at least as long, only spaces after it
				room = n - 8
			}
		}
	}
	if curLen > 0 {
		fenceMark = ""
		if text := strings.TrimRight(cur.String(), "\n"); text != "" {
			out = append(out, text)
		}
	}
	if len(out) == 0 {
		out = []string{s} // whitespace-only input still yields one piece, like the short-input path
	}
	return out
}

// fenceLine reports the code fence a line starts: up to three leading spaces, then >= 3 identical backticks or
// tildes (CommonMark). It returns the run, the trimmed run+info line, and whether only spaces (or a CRLF CR) follow.
func fenceLine(line string) (marker, head string, bare bool) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		if i == 3 { // four or more leading spaces make an indented code block, not a fence
			return "", "", false
		}
		i++
	}
	if i >= len(line) {
		return "", "", false
	}
	c := line[i]
	if c != '`' && c != '~' {
		return "", "", false
	}
	start := i
	for i < len(line) && line[i] == c {
		i++
	}
	if i-start < 3 {
		return "", "", false
	}
	rest := strings.TrimRight(line[i:], " \r") // CommonMark: only spaces may follow the run; CR comes from CRLF
	return line[start:i], strings.TrimSpace(line[start:]), rest == ""
}
