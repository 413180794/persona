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
	Header   string   `json:"header,omitempty"`
	Question string   `json:"question"`
	Detail   string   `json:"detail,omitempty"` // lines above the question that belong to it (the command asked about)
	Options  []Option `json:"options"`
	Rev      string   `json:"rev"`
}

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
	// option takes them before its text, and enter after.
	Keys []string `json:"keys"`
}

// DialogLines is how much of the screen a dialog is read from, the same when the card is made (hub) and when its
// answer is checked (node): the revision covers what the window shows.
const DialogLines = 40

var numberedRE = regexp.MustCompile(`^(\s*)(?:([❯›>])\s*)?(\d)\.\s+(\S.*)$`)

// cursorRE is the selected row of an unnumbered list: `❯ No, exit`.
var cursorRE = regexp.MustCompile(`^(\s*)❯\s+(\S.*)$`)

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
	indent int // column where the label starts
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
		rows = append(rows, row{i: i, n: n, label: strings.TrimSpace(l[m[8]:m[9]]), indent: len([]rune(l[:m[8]]))})
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
		var detail []string
		for _, l := range lines[r.i+1 : end] {
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
		o := Option{Key: strconv.Itoa(r.n), Label: r.label, Detail: joinWrapped(detail), Keys: []string{strconv.Itoa(r.n)}}
		switch lower := strings.ToLower(r.label); {
		case strings.HasPrefix(lower, "type something"):
			o.Kind = KindTypeSomething
		case strings.HasPrefix(lower, "chat about this"):
			o.Kind = KindChat
		}
		d.Options = append(d.Options, o)
	}
	d.describe(lines, block[0].i)
	return d
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
		switch {
		case t == "":
		case strings.ContainsRune("☐□☒✔◻■", []rune(t)[0]):
			d.Header = strings.TrimSpace(string([]rune(t)[1:]))
		case strings.HasPrefix(t, "│") || strings.HasPrefix(t, "|"):
			quoted = append(quoted, strings.TrimSpace(strings.TrimLeft(t, "│|")))
		default:
			plain = append(plain, t)
		}
	}
	switch {
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
		h.Write([]byte("\x00" + o.Key + "\x00" + o.Label + "\x00" + o.Detail))
	}
	d.Rev = hex.EncodeToString(h.Sum(nil))[:12]
}

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
