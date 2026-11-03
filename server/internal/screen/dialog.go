package screen

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Dialog is a choice a terminal agent waits on (AskUserQuestion without the hook, a permission prompt, a picker),
// read off its screen: header chip, question, options, and Rev, a hash of what it asks. The node checks Rev again
// right before pressing keys, so an answer never lands in a dialog that changed after the card was made.
type Dialog struct {
	Header   string `json:"header,omitempty"`
	Question string `json:"question"`
	Detail   string `json:"detail,omitempty"` // lines above the question that belong to it (the command asked about)
	// Warning is what the agent adds in a box above the question about why it asks (Claude Code's 「Dangerous rm
	// operation on possibly-empty variable…」); Deadline its 「⚠ … will automatically block this request in 1:59」,
	// whose countdown moves every second. Neither is in Rev: a card stays answerable while the clock runs.
	Warning  string   `json:"warning,omitempty"`
	Deadline string   `json:"deadline,omitempty"`
	Options  []Option `json:"options"`
	// Multi: a multi-select question (AskUserQuestion's multiSelect, Claude Code 2.1.316): its options are check
	// boxes ("[ ]" / "[✔]") that enter or their number toggles, and the row under them sends what is checked —
	// Submit, which opens the review tab, or Next when another question follows.
	Multi  bool   `json:"multi,omitempty"`
	Submit string `json:"submit,omitempty"` // that row's label: "Submit" or "Next"
	// Steps are the questions of a multi-question AskUserQuestion, from its tab bar ("←  ☒ 环境  ☐ 平台  ✔ Submit  →",
	// the Submit tab left out). A single question's one tab is its Header.
	Steps []Step `json:"steps,omitempty"`
	// Review is the Submit tab ("Review your answers", "1. Submit answers / 2. Cancel"); Incomplete when it warns
	// that a question has no answer yet.
	Review     bool   `json:"review,omitempty"`
	Incomplete bool   `json:"incomplete,omitempty"`
	Rev        string `json:"rev"`
	// Cursor is where the dialog's ❯ is: an option's key, CursorSubmit on the Submit / Next row, "" when unseen.
	Cursor string `json:"-"`
}

// Step is a question of a multi-question dialog: its header, and whether it has an answer.
type Step struct {
	Label string `json:"label"`
	Done  bool   `json:"done"`
}

// CursorSubmit is Dialog.Cursor on a multi-select question's Submit / Next row.
const CursorSubmit = "submit"

// Option kinds besides a plain choice.
const (
	KindTypeSomething = "type_something" // picks a free-text field: answer with text
	KindChat          = "chat"           // "Chat about this": back to the conversation
)

type Option struct {
	Key    string `json:"key"` // its number; for unnumbered dialogs (Claude's trust prompt) its position, 1-based
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Kind   string `json:"kind,omitempty"`
	// Keys pick it: its number, or arrow moves from the cursor plus enter for an unnumbered dialog. A type_something
	// option takes them before its text, and enter after. A multi-select option's number only toggles it.
	Keys []string `json:"keys"`
	// Checked: a multi-select option's box is ticked, or the ✔ after the option an answered question had picked.
	Checked bool `json:"checked,omitempty"`
	// Text is what is typed into a type_something option: the terminal shows it in place of the label.
	Text string `json:"text,omitempty"`
}

// DialogLines is how much of the screen a dialog is read from, the same when the card is made (hub) and when its
// answer is checked (node): the revision covers what the window shows.
const DialogLines = 40

var numberedRE = regexp.MustCompile(`^(\s*)(?:([❯›>])\s*)?(\d)\.\s+(\S.*)$`)

// cursorRE is the selected row of an unnumbered list: `❯ No, exit`.
var cursorRE = regexp.MustCompile(`^(\s*)❯\s+(\S.*)$`)

// boxRE is a multi-select option's check box before its label: `[ ] 手机`, `[✔] 电脑`.
var boxRE = regexp.MustCompile(`^\[([ ✔✓xX])\]\s*(.*)$`)

// submitRE is the row under a multi-select question's options that sends them, maybe under the cursor.
var submitRE = regexp.MustCompile(`^\s*(❯)?\s*(Submit|Next)$`)

// ParseDialog reads the dialog at the bottom of a plain screen, or nil when there is none.
func ParseDialog(plain string) *Dialog {
	lines := strings.Split(strings.ReplaceAll(plain, "\r", ""), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(strings.ReplaceAll(l, " ", " "), " ")
	}
	if d := numbered(lines); d != nil {
		return d
	}
	return unnumbered(lines)
}

type row struct {
	i, n   int
	label  string
	indent int  // column where the label starts
	cursor bool // the ❯ is on it
}

func numbered(lines []string) *Dialog {
	var rows []row
	for i, l := range lines {
		m := numberedRE.FindStringSubmatchIndex(l)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(l[m[6]:m[7]])
		if n < 1 {
			continue
		}
		rows = append(rows, row{i: i, n: n, label: strings.TrimSpace(l[m[8]:m[9]]), indent: len([]rune(l[:m[8]])), cursor: m[4] >= 0})
	}
	// The last run 1, 2, 3… is the live dialog.
	start := -1
	for k := len(rows) - 1; k >= 0; k-- {
		if rows[k].n == 1 {
			start = k
			break
		}
	}
	if start < 0 {
		return nil
	}
	block := []row{rows[start]}
	for _, r := range rows[start+1:] {
		if r.n == block[len(block)-1].n+1 {
			block = append(block, r)
		}
	}
	if len(block) < 2 {
		return nil
	}
	d := &Dialog{}
	for k, r := range block {
		end := min(len(lines), r.i+6)
		if k+1 < len(block) {
			end = block[k+1].i
		}
		o := Option{Key: strconv.Itoa(r.n), Label: r.label, Keys: []string{strconv.Itoa(r.n)}}
		if m := boxRE.FindStringSubmatch(r.label); m != nil {
			d.Multi = true
			o.Label, o.Checked = strings.TrimSpace(m[2]), m[1] != " "
		} else if l, ok := strings.CutSuffix(r.label, " ✔"); ok { // the pick of an answered question, shown again
			o.Label, o.Checked = strings.TrimSpace(l), true
		}
		if r.cursor {
			d.Cursor = o.Key
		}
		var detail []string
		for _, l := range lines[r.i+1 : end] {
			if m := submitRE.FindStringSubmatch(l); m != nil && d.Multi {
				d.Submit = m[2]
				if m[1] != "" {
					d.Cursor = CursorSubmit
				}
				break
			}
			lead := len(l) - len(strings.TrimLeft(l, " "))
			t := strings.TrimSpace(l)
			if t == "" || isRule(t) || lead < r.indent-1 {
				if len(detail) > 0 {
					break
				}
				continue
			}
			detail = append(detail, t)
		}
		o.Detail = joinWrapped(detail)
		switch lower := strings.ToLower(o.Label); {
		case strings.HasPrefix(lower, "type something"):
			o.Kind = KindTypeSomething
		case strings.HasPrefix(lower, "chat about this"):
			o.Kind = KindChat
		}
		d.Options = append(d.Options, o)
	}
	d.typedOption()
	d.describe(lines, block[0].i)
	return d
}

// typedOption: text typed into AskUserQuestion's free-text field shows in place of its "Type something" label (and
// checks it, in a multi-select question). The field is the option above "Chat about this": it keeps its label and
// kind, the text goes in Text, so the dialog's revision does not change with what is typed.
func (d *Dialog) typedOption() {
	for k, o := range d.Options {
		if o.Kind != KindChat || k == 0 {
			continue
		}
		if f := &d.Options[k-1]; f.Kind == "" {
			// As Claude labels it when empty: "Type something." under a single choice.
			label := "Type something"
			if !d.Multi {
				label += "."
			}
			f.Text, f.Label, f.Kind = f.Label, label, KindTypeSomething
		}
	}
}

// unnumbered reads a list without numbers whose cursor row is `❯ label` and whose other rows line up with it, under
// a footer of key hints (Claude's trust prompt): options are picked by arrow moves and enter.
func unnumbered(lines []string) *Dialog {
	if !Choosing(strings.Join(lines, "\n")) {
		return nil
	}
	cur := -1
	indent := 0
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-30; i-- {
		if m := cursorRE.FindStringSubmatch(lines[i]); m != nil {
			cur, indent = i, len([]rune(m[1]))+2
			break
		}
	}
	if cur < 0 {
		return nil
	}
	sibling := func(l string) bool {
		r := []rune(l)
		return len(r) > indent && strings.TrimSpace(string(r[:indent])) == "" && r[indent] != ' ' && !footerRE.MatchString(l)
	}
	first, last := cur, cur
	for first-1 >= 0 && sibling(lines[first-1]) {
		first--
	}
	for last+1 < len(lines) && sibling(lines[last+1]) {
		last++
	}
	if first == last {
		return nil
	}
	d := &Dialog{}
	for i := first; i <= last; i++ {
		label := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "❯"))
		var keys []string
		for k := cur; k < i; k++ {
			keys = append(keys, "down")
		}
		for k := i; k < cur; k++ {
			keys = append(keys, "up")
		}
		d.Options = append(d.Options, Option{Key: strconv.Itoa(i - first + 1), Label: label, Keys: append(keys, "enter")})
	}
	d.describe(lines, first)
	return d
}

// tabsRE is AskUserQuestion's tab bar: "←  ☒ 环境  ☐ 平台  ✔ Submit  →" (a multi-select or multi-question dialog).
var tabsRE = regexp.MustCompile(`^←\s+(.*?)\s+→$`)

// gapRE parts the tab bar's tabs (two spaces or more; a header may have one).
var gapRE = regexp.MustCompile(`\s{2,}`)

// The review tab's lines (Claude Code 2.1.316).
const (
	reviewTitle      = "Review your answers"
	reviewIncomplete = "You have not answered all questions"
)

// steps reads the tab bar: each question's header and whether it is answered (☒), the Submit tab left out.
func steps(bar string) []Step {
	var out []Step
	for _, f := range gapRE.Split(bar, -1) {
		r := []rune(strings.TrimSpace(f))
		if len(r) < 2 || !strings.ContainsRune("☐☒✔", r[0]) {
			continue
		}
		label := strings.TrimSpace(string(r[1:]))
		if r[0] == '✔' && label == "Submit" {
			continue
		}
		out = append(out, Step{Label: label, Done: r[0] == '☒'})
	}
	return out
}

// describe fills header, question and detail from the lines above the options, up to the rule that opens the dialog.
func (d *Dialog) describe(lines []string, first int) {
	top := 0
	for i := first - 1; i >= 0; i-- {
		if isRule(lines[i]) {
			top = i + 1
			break
		}
	}
	var quoted, plain []string
	for _, l := range lines[top:first] {
		t := strings.TrimSpace(l)
		m := tabsRE.FindStringSubmatch(t)
		switch {
		case t == "" || dashedRE.MatchString(t): // the dashed rules around a command (╌╌╌)
		case deadlineRE.MatchString(t):
			d.Deadline = strings.TrimSpace(strings.TrimPrefix(t, "⚠"))
		case m != nil:
			// One question: its tab is the header. Several: which one is shown is only told by color, so no header;
			// the steps say which are answered.
			if st := steps(m[1]); len(st) == 1 {
				d.Header = st[0].Label
			} else {
				d.Steps = st
			}
		case t == reviewTitle:
			d.Review = true
		case strings.HasSuffix(t, reviewIncomplete):
			d.Incomplete = true
		case strings.ContainsRune("☐□☒✔◻■", []rune(t)[0]):
			d.Header = strings.TrimSpace(string([]rune(t)[1:]))
		case strings.HasPrefix(t, "│") || strings.HasPrefix(t, "|"):
			quoted = append(quoted, strings.TrimSpace(strings.TrimLeft(t, "│|")))
		default:
			plain = append(plain, t)
		}
	}
	asks := len(plain) > 0 && strings.ContainsAny(plain[len(plain)-1], "?？")
	switch {
	case len(quoted) > 0 && asks:
		// A boxed note above a question that is asked in plain words (a permission prompt's warning): the note is
		// the warning, the question is the question.
		d.Warning = joinWrapped(quoted)
		d.Question = plain[len(plain)-1]
		d.Detail = strings.Join(plain[:len(plain)-1], "\n")
	case len(quoted) > 0:
		d.Question = joinWrapped(quoted)
		d.Detail = strings.Join(plain, "\n")
	case len(plain) > 0:
		// The last line that asks something (a trust prompt ends in a "Security guide" link), else the last line.
		q := len(plain) - 1
		for i := len(plain) - 1; i >= 0; i-- {
			if strings.ContainsAny(plain[i], "?？") {
				q = i
				break
			}
		}
		d.Question = plain[q]
		d.Detail = strings.Join(append(slices.Clone(plain[:q]), plain[q+1:]...), "\n")
	default:
		d.Question, d.Header = d.Header, ""
	}
	h := sha256.New()
	h.Write([]byte(d.Header + "\x00" + d.Question + "\x00" + d.Detail))
	for _, o := range d.Options {
		label := o.Label
		if o.Kind == KindTypeSomething { // its label is what is typed into it until it is read back as Text
			label = o.Kind
		}
		h.Write([]byte("\x00" + o.Key + "\x00" + label + "\x00" + o.Detail))
	}
	d.Rev = hex.EncodeToString(h.Sum(nil))[:12]
}

// The countdown Claude Code adds when it will answer for the user (auto mode: 「⚠ Claude Code will automatically
// block this request in 1:59, to avoid holding up an unattended session」; earlier builds say deny), and the dashed
// rules it draws around the command asked about.
var (
	deadlineRE = regexp.MustCompile(`(?i)^⚠?\s*.*\bwill automatically (block|deny|allow|approve|accept|reject)\b.*\bin \d+:\d{2}\b`)
	dashedRE   = regexp.MustCompile(`^[╌┄┈╍┅┉-]{8,}$`)
)

// Option by key, or nil.
func (d *Dialog) Option(key string) *Option {
	for i := range d.Options {
		if d.Options[i].Key == key {
			return &d.Options[i]
		}
	}
	return nil
}

// joinWrapped puts rows the terminal wrapped back into one text: no space where a row ends inside CJK text.
func joinWrapped(rows []string) string {
	var b strings.Builder
	var last rune
	for _, r := range rows {
		if r == "" {
			continue
		}
		next := []rune(r)[0]
		if b.Len() > 0 && !(cjk(last) || cjk(next)) {
			b.WriteByte(' ')
		}
		b.WriteString(r)
		rs := []rune(r)
		last = rs[len(rs)-1]
	}
	return b.String()
}

func cjk(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		(r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}
