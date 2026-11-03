package node

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/screen"
)

// tui plays a multi-select question the way Claude Code 2.1.316 does (measured in herdr): a digit toggles its option
// from an option's row, is typed into the free-text field on its row and does nothing on the Submit row; up / down move over
// the options, the field and the Submit row (and Chat below it); typing into the field checks it, clearing unchecks it.
type tui struct {
	d     *screen.Dialog
	keys  []string
	typed bool
}

func (t *tui) row() []string {
	var rows []string
	for _, o := range t.d.Options {
		rows = append(rows, o.Key)
		if o.Kind == screen.KindTypeSomething {
			rows = append(rows, screen.CursorSubmit)
		}
	}
	return rows
}

func (t *tui) field() *screen.Option {
	for i := range t.d.Options {
		if t.d.Options[i].Kind == screen.KindTypeSomething {
			return &t.d.Options[i]
		}
	}
	return nil
}

func (t *tui) press(tst *testing.T, m multiMove) {
	tst.Helper()
	f := t.field()
	if m.text != "" {
		if t.d.Cursor != f.Key {
			tst.Fatalf("typed %q off the field (cursor %s)", m.text, t.d.Cursor)
		}
		f.Text, f.Checked = m.text, true
		t.keys = append(t.keys, "text")
		t.typed = true
		return
	}
	for _, k := range m.keys {
		t.keys = append(t.keys, k)
		rows := t.row()
		at := slices.Index(rows, t.d.Cursor)
		switch k {
		case "up":
			t.d.Cursor = rows[max(at-1, 0)]
		case "down":
			t.d.Cursor = rows[min(at+1, len(rows)-1)]
		case "ctrl+e":
		case "ctrl+u":
			f.Text, f.Checked = "", false
		default:
			if t.d.Cursor == f.Key {
				tst.Fatalf("digit %s typed into the field", k)
			}
			if t.d.Cursor == screen.CursorSubmit {
				tst.Fatalf("digit %s pressed on the Submit row, which ignores it", k)
			}
			o := t.d.Option(k)
			o.Checked = !o.Checked
		}
	}
}

func dialogFixture(t *testing.T, name string) *screen.Dialog {
	t.Helper()
	b, err := os.ReadFile("../screen/testdata/claude-2.1.316-" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	d := screen.ParseDialog(string(b))
	if d == nil || !d.Multi {
		t.Fatalf("%s: not a multi-select dialog", name)
	}
	return d
}

// From whatever the terminal shows — the cursor on an option, on the free-text field holding text, on Submit; boxes
// ticked at the computer — the answer is reached with a key per box that differs, the moves to the field and to
// Submit, and enter; a digit is never typed into the field.
func TestMultiSelectMoves(t *testing.T) {
	for _, c := range []struct {
		name, fixture string
		want          []string
		text          string
		keys          string // what is pressed, "text" for the typed answer, then enter
	}{
		{"nothing ticked, two wanted", "multiselect-100", []string{"2", "4"}, "", "2 4 down down down down down"},
		{"ticked at the computer: 1 and 3, cursor on 1; want 1 and 2", "multiselect-checked-56", []string{"1", "2"}, "", "2 3 down down down down down"},
		{"ticked 1 and 3 and text, cursor on the field; want only 2", "multiselect-typed-100", []string{"2"}, "", "up 1 2 3 down ctrl+e ctrl+u down"},
		{"the same text kept", "multiselect-typed-100", []string{"1", "3"}, "示例自由回答", "down"},
		{"other text", "multiselect-typed-100", []string{"1", "3"}, "另一个回答", "ctrl+e ctrl+u text down"},
		{"cursor on Submit, add text", "multiselect-on-submit-100", []string{"1", "3"}, "新的", "up text down"},
		{"cursor on Submit, already right", "multiselect-on-submit-100", []string{"1", "3"}, "", ""},
		{"cursor on Submit, other ticks and text", "multiselect-on-submit-100", []string{"2"}, "补充一句", "up up 1 2 3 down text down"},
		{"next question of several", "questions-multi-next-100", []string{"2"}, "", "1 2 3 down down down down"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := dialogFixture(t, c.fixture)
			want := map[string]bool{}
			for _, k := range c.want {
				want[k] = true
			}
			term := &tui{d: d}
			for i := 0; ; i++ {
				if i > 30 {
					t.Fatalf("no end: %v", term.keys)
				}
				m, err := nextMultiMove(d, want, c.text)
				if err != nil {
					t.Fatal(err)
				}
				if m.submit {
					break
				}
				term.press(t, m)
			}
			if got := strings.Join(term.keys, " "); got != c.keys {
				t.Fatalf("pressed %q, want %q", got, c.keys)
			}
			for _, o := range d.Options {
				if o.Kind == "" && o.Checked != want[o.Key] {
					t.Fatalf("option %s checked %v", o.Key, o.Checked)
				}
			}
			if f := term.field(); f.Text != c.text || f.Checked != (c.text != "") {
				t.Fatalf("field %+v", f)
			}
		})
	}
	// The field ticked with nothing in it (its digit pressed at the computer) and no text wanted: unticked.
	d := dialogFixture(t, "multiselect-100")
	d.Options[4].Checked = true
	if m, err := nextMultiMove(d, map[string]bool{}, ""); err != nil || !slices.Equal(m.keys, []string{"5"}) {
		t.Fatalf("%+v %v", m, err)
	}
	d.Cursor = ""
	if _, err := nextMultiMove(d, map[string]bool{}, ""); err == nil {
		t.Fatal("no cursor, no move")
	}
}
