package transcript

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/nwverse/persona/server/internal/proto"
)

// Claude Code's transcript is a tree: every line has a uuid and a parentUuid. A conversation is mostly one chain, but
// going back to an earlier message (/rewind, editing a sent message with ↑) writes the next message as a second child
// of the line before the one it replaces. The file is only ever appended to, so the turns that were gone back over
// stay in it, and a reader that takes the lines in order shows both. Claude itself continues from the last line
// written.
//
// What is followed here is the narrowest thing that identifies this: two of the user's own messages hanging from the
// same parent, the earlier one having been answered (an assistant line grew from it). That one, and everything that
// grew from it, is an abandoned branch and is left out of the items (the later one may itself be abandoned by a
// third). An earlier message nothing answered is not one: pressing Esc on a message that has no reply yet and sending
// it again (or another) writes the same two siblings, and there is nothing that was gone back over — hiding it made the
// history token move for no reason and cut the interrupted turn from the chat (#3036); the terminal shows it, so
// does the chat. Other forks are ordinary — parallel tool results, an attachment beside a reply, a hook's line — and a
// compaction starts a new chain (parentUuid null) on purpose, so the walk back from the last line, which is what a
// resume does, would lose them: it is not used.
//
// Clients hold items they were already sent. A branch is abandoned when the next message is written (or when the node
// itself rewound, File.Rewound), so the history token (Span.History) carries how many abandoned branches the file has:
// it changes then, and a client that sees it change starts over (docs/protocol.md「items 的连续性」), as it does for a
// file that was rewritten. The token is a function of the file, so it is the same across node restarts.

// treeLine is what parseLine saw of a line's place in the tree.
type treeLine struct {
	uuid, parent string
	sidechain    bool
	assistant    bool // the line is the assistant's (a reply, a tool call): the message it grew from was answered
}

// treeRec is a line with a uuid, as the tree keeps it: prompt says it is one of the user's own messages, reply that it
// is the assistant's.
type treeRec struct {
	uuid, parent string
	prompt       bool
	reply        bool
}

// branchTree tracks the lines read so far (file order) and which of them lie on abandoned branches.
type branchTree struct {
	recs    []treeRec
	replies map[string]bool     // lines that are the assistant's
	kids    map[string][]string // parent → the lines hanging from it
	last    map[string]string   // parent → the latest message of the user's hanging from it
	hidden  map[string]bool     // lines on abandoned branches
	roots   map[string]bool     // where each abandoned branch starts
	manual  map[string]bool     // roots set by File.Rewound: they survive a rebuild
}

func (t *branchTree) init() {
	if t.kids == nil {
		t.kids, t.last, t.hidden, t.roots = map[string][]string{}, map[string]string{}, map[string]bool{}, map[string]bool{}
		t.replies = map[string]bool{}
	}
}

// add takes the next line. drop says it hangs from an abandoned branch (its items are not shown); newly lists the
// lines it made abandoned (the earlier message of the user's with the same parent, and what grew from it).
func (t *branchTree) add(r treeRec) (drop bool, newly []string) {
	t.init()
	t.recs = append(t.recs, r)
	t.kids[r.parent] = append(t.kids[r.parent], r.uuid)
	if r.reply {
		t.replies[r.uuid] = true
	}
	if t.hidden[r.uuid] {
		return true, nil // a line repeated in the file (a resume copies lines)
	}
	if r.parent != "" && t.hidden[r.parent] {
		t.hidden[r.uuid] = true
		return true, nil
	}
	if !r.prompt || r.parent == "" {
		return false, nil
	}
	prev := t.last[r.parent]
	t.last[r.parent] = r.uuid
	if prev == "" || prev == r.uuid || t.hidden[prev] || !t.answered(prev) {
		return false, nil
	}
	t.roots[prev] = true
	return false, t.hideFrom(prev)
}

// answered says an assistant line grew from a line (or from what grew from it): there is a turn that would be gone back
// over. It looks at the lines read so far, which for a line already there is everything its branch wrote.
func (t *branchTree) answered(uuid string) bool {
	seen := map[string]bool{}
	for todo := []string{uuid}; len(todo) > 0; {
		u := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[u] {
			continue
		}
		seen[u] = true
		if t.replies[u] {
			return true
		}
		todo = append(todo, t.kids[u]...)
	}
	return false
}

// hideFrom marks a line and everything that grew from it abandoned; it returns the ones that were not yet.
func (t *branchTree) hideFrom(uuid string) []string {
	var out []string
	for todo := []string{uuid}; len(todo) > 0; {
		u := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if t.hidden[u] {
			continue
		}
		t.hidden[u] = true
		out = append(out, u)
		todo = append(todo, t.kids[u]...)
	}
	return out
}

// prepend adds lines that come before the ones read so far (a window earlier in the file) and works the whole tree
// out again, which a branch across the two parts needs. It returns the lines that are newly abandoned.
func (t *branchTree) prepend(older []treeRec) []string {
	if len(older) == 0 {
		return nil
	}
	was, manual := t.hidden, t.manual
	recs := append(append([]treeRec(nil), older...), t.recs...)
	*t = branchTree{manual: manual}
	for _, r := range recs {
		t.add(r)
	}
	for u := range t.manual {
		if _, ok := t.kids[u]; ok || t.hasRec(u) {
			t.roots[u] = true
			t.hideFrom(u)
		}
	}
	var newly []string
	for u := range t.hidden {
		if !was[u] {
			newly = append(newly, u)
		}
	}
	return newly
}

func (t *branchTree) hasRec(uuid string) bool {
	for _, r := range t.recs {
		if r.uuid == uuid {
			return true
		}
	}
	return false
}

// uuidOf is the transcript line an item id belongs to ("<uuid>#<n>"; a line without a uuid is "line<seq>#<n>").
func uuidOf(id string) string {
	if i := strings.LastIndexByte(id, '#'); i > 0 {
		return id[:i]
	}
	return id
}

// isMessage says the items of a line are one of the user's own messages: typed or sent, not a tool result, a
// notification a background task woke the agent with, or another agent's report.
func isMessage(items []proto.Item) bool {
	for _, it := range items {
		if it.Role == "user" && it.Kind == proto.KindText && !it.Queued && !it.Wake && it.From == "" {
			return true
		}
	}
	return false
}

// withoutLines drops the items of lines in gone.
func withoutLines(items []proto.Item, gone map[string]bool) []proto.Item {
	if len(gone) == 0 || len(items) == 0 {
		return items
	}
	out := items[:0:0]
	for _, it := range items {
		if !gone[uuidOf(it.ID)] {
			out = append(out, it)
		}
	}
	return out
}

func setOf(uuids []string) map[string]bool {
	m := make(map[string]bool, len(uuids))
	for _, u := range uuids {
		m[u] = true
	}
	return m
}

// trackLine puts a line just parsed into the tree. drop says it is on an abandoned branch: its items must not be
// kept. When it abandons an earlier branch, that branch's items are taken out of *kept and *sent (either may be nil).
func trackLine(t *branchTree, meta *Meta, items []proto.Item, kept, sent *[]proto.Item) (drop bool) {
	tl := meta.tree
	meta.tree = treeLine{}
	if tl.uuid == "" || tl.sidechain || meta.Sidechain || meta.Codex {
		return false
	}
	drop, newly := t.add(treeRec{uuid: tl.uuid, parent: tl.parent, prompt: isMessage(items), reply: tl.assistant})
	if len(newly) > 0 {
		gone := setOf(newly)
		if kept != nil {
			*kept = withoutLines(*kept, gone)
		}
		if sent != nil {
			*sent = withoutLines(*sent, gone)
		}
	}
	return drop
}

// squash drops every space: the words of a message, as the user typed them.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// Rewound is told by the node that the session was rewound in its terminal to before a message of the user's: the
// occurrence-th one with these words counting from the end. Claude writes nothing for that until the next message, so
// from now on that message and everything after it is left out here too. It says whether it found the message, and
// where the transcript stands (the token has changed: clients start over).
func (f *File) Rewound(prompt string, occurrence int) (Span, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := squash(prompt)
	if f.tree == nil || want == "" || occurrence < 1 {
		return Span{}, false
	}
	seen := 0
	for i := len(f.items) - 1; i >= 0; i-- {
		it := f.items[i]
		if it.Role != "user" || it.Kind != proto.KindText || it.Queued || it.Wake || it.From != "" || squash(it.Text) != want {
			continue
		}
		if seen++; seen < occurrence {
			continue
		}
		uuid := uuidOf(it.ID)
		if !f.tree.hasRec(uuid) {
			return Span{}, false
		}
		if f.tree.manual == nil {
			f.tree.manual = map[string]bool{}
		}
		f.tree.manual[uuid], f.tree.roots[uuid] = true, true
		f.items = withoutLines(f.items, setOf(f.tree.hideFrom(uuid)))
		return Span{f.history(), f.lines, f.lines}, true
	}
	return Span{}, false
}

// branchToken is the part of the history token that says how many abandoned branches the file has.
func (f *File) branchToken() string {
	if f.tree == nil || len(f.tree.roots) == 0 {
		return ""
	}
	return fmt.Sprintf(".r%d", len(f.tree.roots))
}
