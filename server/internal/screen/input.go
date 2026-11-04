// Package screen reads Claude Code / Codex terminal screens (herdr pane reads): the input box and the dialogs.
//
// The input-box rules are ported from Ovis (github.com/devswha/ovis, server/claude-input.ts, MIT License,
// Copyright (c) 2026 devswha); see THIRD_PARTY_NOTICES.md.
package screen

import (
	"regexp"
	"strings"
)

var (
	ansiRE  = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	oscRE   = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	solidRE = regexp.MustCompile(`^[─━]{8,}$`)
	// A labeled rule over the input box: a named session (`──── my-session ─`).
	labeledRE = regexp.MustCompile(`^─{2,}\s.*─$`)
	// Claude's IDE pill right after the ❯ (`[⧉ 3 lines selected] `): not input, and clearing keys leave it.
	idePillRE = regexp.MustCompile(`^[ \x{00a0}]?\[⧉ [^\]\n]+\] ?`)
	// A paste or an image Claude folded into a placeholder is content, however it is colored.
	placeholderRE = regexp.MustCompile(`\[(?:Pasted text #\d+|Image #\d+)`)
	// The new-session tip in an empty box, `Try "how does <filepath> work?"`, drawn without color under NO_COLOR.
	tipRowRE = regexp.MustCompile(`^Try "[^"]+"$`)
)

// Plain drops ANSI escapes and carriage returns.
func Plain(s string) string {
	s = oscRE.ReplaceAllString(ansiRE.ReplaceAllString(s, ""), "")
	return strings.ReplaceAll(s, "\r", "")
}

func isRule(line string) bool {
	t := strings.TrimSpace(line)
	return solidRE.MatchString(t) || labeledRE.MatchString(t)
}

// box is Claude Code's input box: the rows between the screen's last two rules.
type box struct {
	plain, raw []string
	clipped    bool // only the bottom rule is on screen (a draft taller than the pane)
}

func inputBox(screen string) *box {
	raw := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	plain := make([]string, len(raw))
	for i, l := range raw {
		plain[i] = strings.TrimRight(Plain(l), " ")
	}
	end := len(plain) - 1
	for end >= 0 && !isRule(plain[end]) {
		end--
	}
	if end < 0 {
		return nil
	}
	start := end - 1
	for start >= 0 && !isRule(plain[start]) {
		start--
	}
	if start < 0 {
		return &box{clipped: true}
	}
	return &box{plain: plain[start+1 : end], raw: raw[start+1 : end]}
}

// AboveInputBox is the screen's rows above Claude Code's input box, as plain text (every row when no box is on
// screen): where its status line is (node/claudestatus.go).
func AboveInputBox(screen string) []string {
	lines := strings.Split(Plain(screen), "\n")
	for i, rules := len(lines)-1, 0; i >= 0; i-- {
		if isRule(lines[i]) {
			if rules++; rules == 2 {
				return lines[:i]
			}
		}
	}
	return lines
}

// run is a stretch of characters drawn with the same dim / inverse state.
type run struct {
	text         string
	dim, inverse bool
}

var escRE = regexp.MustCompile(`\x1b(?:\[([0-?]*)[ -/]*([@-~])|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

// sgrRuns splits an ANSI line by dim (SGR 2) and inverse (SGR 7); 38/48/58 colors are skipped whole, so the 2 of
// `38;2;r;g;b` is not dim.
func sgrRuns(line string) []run {
	var out []run
	dim, inverse := false, false
	off := 0
	for _, m := range escRE.FindAllStringSubmatchIndex(line, -1) {
		if m[0] > off {
			out = append(out, run{line[off:m[0]], dim, inverse})
		}
		off = m[1]
		if m[4] < 0 || line[m[4]:m[5]] != "m" {
			continue
		}
		params := "0"
		if m[2] >= 0 && m[3] > m[2] {
			params = line[m[2]:m[3]]
		}
		codes := strings.Split(params, ";")
		for i := 0; i < len(codes); i++ {
			c := atoi(codes[i])
			switch c {
			case 38, 48, 58:
				if i+1 < len(codes) {
					switch atoi(codes[i+1]) {
					case 5:
						i += 2
					case 2:
						i += 4
					}
				}
			case 0:
				dim, inverse = false, false
			case 22:
				dim = false
			case 2:
				dim = true
			case 27:
				inverse = false
			case 7:
				inverse = true
			}
		}
	}
	if off < len(line) {
		out = append(out, run{line[off:], dim, inverse})
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func blank(r rune) bool { return r == ' ' || r == '\t' || r == ' ' }

// colorless: the read sets no color and no dim anywhere (Claude Code under NO_COLOR).
func colorless(ansi string) bool {
	for _, m := range regexp.MustCompile(`\x1b\[([0-9;:]*)m`).FindAllStringSubmatch(ansi, -1) {
		for _, f := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ';' || r == ':' }) {
			c := atoi(f)
			if c == 2 || (c >= 30 && c <= 38) || (c >= 40 && c <= 48) || (c >= 90 && c <= 97) || (c >= 100 && c <= 107) {
				return false
			}
		}
	}
	return true
}

// Input is what Claude Code's input box holds.
type Input struct {
	// Box: the screen has an input box (rules at all). Without one nothing is known about the input.
	Box bool
	// Draft: the box is not known to be empty — typed text, bash mode, a paste placeholder, a box clipped by the pane,
	// or rows that are not a `❯` input. Claude's own grey text (a suggested prompt, its tip) and its cursor are not.
	Draft bool
	// Text is the box's content as shown (plain, rows joined, without the ❯), for telling the user what is there.
	Text string
	// Clearable is the draft a single-row box holds, which ctrl+k ctrl+u empty; "" when it cannot be cleared that way.
	Clearable string
	// Suggestion is the next message Claude proposes after a turn (grey in an otherwise empty box: Tab takes it,
	// Enter sends it); rows it wraps onto are joined with a space. Not the new-session tip (`Try "…"`).
	Suggestion string
}

// ClaudeInput reads the live input box. ansi is the same screen read with colors ("" when it could not be read):
// only colors tell grey suggestions from typed words, so without them any text counts as a draft.
func ClaudeInput(ansi string) Input {
	b := inputBox(ansi)
	if b == nil {
		return Input{}
	}
	in := Input{Box: true}
	if b.clipped {
		in.Draft = true
		return in
	}
	rows := make([]string, len(b.plain))
	for i, r := range b.plain {
		rows[i] = strings.TrimRight(strings.ReplaceAll(r, " ", " "), " ")
	}
	// Without Claude's ❯ (bash mode's ! stays: it says the text would run as a command).
	in.Text = strings.TrimSpace(strings.TrimPrefix(strings.Join(rows, "\n"), "❯"))
	if len(b.plain) == 0 || !strings.HasPrefix(b.plain[0], "❯") {
		in.Draft = true
		return in
	}
	after := strings.TrimPrefix(b.plain[0], "❯")
	afterPill := idePillRE.ReplaceAllString(after, " ")
	rest := strings.Join(append([]string{afterPill}, b.plain[1:]...), "\n")
	if strings.TrimSpace(strings.ReplaceAll(rest, " ", " ")) == "" {
		in.Text = ""
		return in
	}
	if placeholderRE.MatchString(strings.Join(b.plain, " ")) {
		in.Draft = true
		return in
	}
	if len(b.plain) == 1 && tipRowRE.MatchString(strings.TrimSpace(strings.ReplaceAll(afterPill, " ", " "))) && colorless(ansi) {
		return in // the tip under NO_COLOR
	}
	in.Draft = typed(b, after)
	if !in.Draft {
		in.Suggestion = suggestion(b, after)
	}
	if in.Draft && len(b.plain) == 1 {
		row := Plain(b.raw[0])
		a := strings.TrimPrefix(row, "❯")
		if pill := idePillRE.FindString(a); pill != "" {
			a = a[len(pill):]
		} else {
			a = strings.TrimPrefix(strings.TrimPrefix(a, " "), " ")
		}
		if strings.TrimSpace(a) != "" {
			in.Clearable = strings.ReplaceAll(a, " ", " ")
		}
	}
	return in
}

// typed: anything after the ❯ that is not dim, other than Claude's own cursor (inverse, on the first grey character).
func typed(b *box, after string) bool {
	pill := len([]rune(idePillRE.FindString(after)))
	prompt, first, cursor, grey := false, true, false, false
	for _, row := range b.raw {
		for _, r := range sgrRuns(row) {
			for _, c := range r.text {
				if !prompt {
					prompt = c == '❯'
					continue
				}
				if pill > 0 {
					pill--
					continue
				}
				if blank(c) {
					continue
				}
				switch {
				case r.dim:
					grey = true
				case first && r.inverse:
					cursor = true
				default:
					return true
				}
				first = false
			}
		}
	}
	return cursor && !grey // a cursor over a typed character has nothing grey after it
}

// suggestion is the grey text after the ❯ of a box with nothing typed (Claude's cursor may sit inverse on its first
// character), each row trimmed and the rows joined with a space; "" for the new-session tip.
func suggestion(b *box, after string) string {
	pill := len([]rune(idePillRE.FindString(after)))
	prompt, first := false, true
	var rows []string
	for _, row := range b.raw {
		var sb strings.Builder
		for _, r := range sgrRuns(row) {
			for _, c := range r.text {
				if !prompt {
					prompt = c == '❯'
					continue
				}
				if pill > 0 {
					pill--
					continue
				}
				if blank(c) {
					sb.WriteRune(' ')
					continue
				}
				if r.dim || (first && r.inverse) {
					sb.WriteRune(c)
				}
				first = false
			}
		}
		if t := strings.TrimSpace(sb.String()); t != "" {
			rows = append(rows, t)
		}
	}
	s := strings.Join(rows, " ")
	if tipRowRE.MatchString(s) {
		return ""
	}
	return s
}

// ClearKeys empty a one-row Claude input box: the row after the cursor, then before it (Ovis, measured on 2.1.295).
var ClearKeys = []string{"ctrl+k", "ctrl+u"}

// selectedRE is a cursor on a numbered choice: `❯ 1. Yes` (Claude), `› 1. Allow` (Codex).
var selectedRE = regexp.MustCompile(`^\s*[❯›>]\s*\d+\.\s+\S`)

// footerRE is a dialog's key hint line: `Enter to confirm · Esc to cancel`, `Esc to cancel` under a picker.
var footerRE = regexp.MustCompile(`(?i)\b(?:esc to (?:cancel|go back|close)|enter to (?:confirm|select|set))\b`)

// Choosing: the bottom of the screen is a dialog — a key hint footer, or a numbered choice with a cursor below the
// last rule (above it is history: an old message may read `❯ 1. …`) — where typed text would pick an option or land
// in the dialog's own field.
func Choosing(plain string) bool {
	lines := strings.Split(strings.ReplaceAll(plain, "\r", ""), "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < 4; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		if footerRE.MatchString(lines[i]) {
			return true
		}
	}
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-30; i-- {
		if isRule(lines[i]) {
			return false
		}
		if selectedRE.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// Refusal codes: why text must not be typed into a pane now.
const (
	DialogOpen = "dialog_open" // a dialog has the keyboard
	InputDraft = "input_draft" // the input box holds something the text would be appended to
	// PromptChanged refuses an answer to a dialog no longer on screen as its card showed it (Dialog.Rev).
	PromptChanged = "prompt_changed"
)

// CheckClaude says whether typing a message into a Claude Code pane is safe: "" when it is, else the refusal code
// and what the input box holds. A screen without an input box or dialog (not drawn yet, not Claude's) is let through.
func CheckClaude(ansi string) (string, Input) {
	if Choosing(Plain(ansi)) {
		return DialogOpen, Input{}
	}
	in := ClaudeInput(ansi)
	if in.Draft {
		return InputDraft, in
	}
	return "", in
}
