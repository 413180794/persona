package kb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// named is a source of a name only (what Ingest needs).
type named string

func (n named) Name() string                                   { return string(n) }
func (named) Chats(context.Context, int) ([]SourceChat, error) { return nil, nil }
func (named) Messages(context.Context, SourceChat, string, int) ([]SourceMessage, string, bool, error) {
	return nil, "", false, nil
}
func (named) Download(context.Context, string, string) error { return nil }

func TestHashHandle(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	same := [][2]Handle{
		{{HandleEmail, "Alice@Example.com "}, {HandleEmail, "alice@example.com"}},
		{{HandlePhone, "+1 (555) 010-0100"}, {HandlePhone, "+15550100100"}},
	}
	for _, p := range same {
		if a, b := HashHandle(key, p[0]), HashHandle(key, p[1]); a == "" || a != b {
			t.Errorf("%v and %v: %q %q", p[0], p[1], a, b)
		}
	}
	if HashHandle(key, Handle{HandleGitHub, "alice"}) == HashHandle(key, Handle{HandleTelegram, "alice"}) {
		t.Error("kinds share a hash")
	}
	if h := HashHandle(key, Handle{HandleEmail, "alice@example.com"}); !strings.HasPrefix(h, "email:") || len(h) != len("email:")+32 ||
		strings.Contains(h, "alice") {
		t.Errorf("hash %q", h)
	}
	if HashHandle(nil, Handle{HandleEmail, "a@b"}) != "" || HashHandle(key, Handle{HandlePhone, "+ ()"}) != "" {
		t.Error("hashed without a key or a value")
	}
	if HashHandle([]byte("another key, also 32 bytes long!"), Handle{HandleEmail, "a@b"}) == HashHandle(key, Handle{HandleEmail, "a@b"}) {
		t.Error("the key does not matter")
	}
}

// A sender's hints are kept hashed in handles.json, the same address from two sources hashing the same; the raw
// address or number is written nowhere, and meta.json (what kb_get and search show) has none of them.
func TestPeopleKeepHashedHandles(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: st, HandleKey: []byte("0123456789abcdef0123456789abcdef")}
	ctx := context.Background()
	at := time.Now().Add(-time.Hour)
	// iMessage: the id is a hash already; the hint is the raw number and address.
	if _, err := e.Ingest(ctx, named("imessage"), SourceChat{ID: "c1", Name: "示例"}, []SourceMessage{
		{ID: "1", SenderID: "0a1b2c", SenderName: "张三", Text: "在吗", At: at, Type: "TEXT",
			SenderHandles: []Handle{{HandlePhone, "+1 555 010 0199"}, {HandleEmail, "Zhang@Example.com"}}},
	}); err != nil {
		t.Fatal(err)
	}
	// mail: the same address.
	if _, err := e.Ingest(ctx, named("mail"), SourceChat{ID: "acc-zhang", Name: "张三 · 邮件"}, []SourceMessage{
		{ID: "2", SenderID: "zhang@example.com", SenderName: "张三", Text: "附件", At: at, Type: "TEXT",
			SenderHandles: []Handle{{HandleEmail, "zhang@example.com"}}},
	}); err != nil {
		t.Fatal(err)
	}
	im, ml := st.PersonHandles(PersonID("imessage", "0a1b2c")), st.PersonHandles(PersonID("mail", "zhang@example.com"))
	if len(im) != 2 || len(ml) != 1 || !strings.Contains(strings.Join(im, " "), ml[0]) {
		t.Fatalf("imessage %v, mail %v", im, ml)
	}
	// Seen again: nothing added.
	_, _ = e.Ingest(ctx, named("mail"), SourceChat{ID: "acc-zhang", Name: "张三 · 邮件"}, []SourceMessage{
		{ID: "3", SenderID: "zhang@example.com", SenderName: "张三", Text: "再发一次", At: at.Add(time.Minute), Type: "TEXT",
			SenderHandles: []Handle{{HandleEmail, "ZHANG@example.com"}}}})
	if got := st.PersonHandles(PersonID("mail", "zhang@example.com")); len(got) != 1 {
		t.Fatalf("added again: %v", got)
	}
	_ = filepath.WalkDir(st.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), "555") || strings.Contains(string(raw), "0199") {
			t.Errorf("%s holds the number", p)
		}
		if filepath.Base(p) == "meta.json" && strings.Contains(string(raw), "email:") {
			t.Errorf("%s holds a hint", p)
		}
		return nil
	})
	d, err := e.Get(KindPerson, PersonID("imessage", "0a1b2c"), 1000)
	if err != nil || strings.Contains(string(d.Meta), "email:") || strings.Contains(string(d.Meta), "phone:") {
		t.Fatalf("kb_get shows hints: %v %s", err, d.Meta)
	}
	// Without the hub's key nothing is kept.
	e.HandleKey = nil
	_, _ = e.Ingest(ctx, named("github"), SourceChat{ID: "pr1", Name: "PR"}, []SourceMessage{
		{ID: "4", SenderID: "alice", SenderName: "alice", Text: "LGTM", At: at, Type: "TEXT", SenderHandles: []Handle{{HandleGitHub, "alice"}}}})
	if got := st.PersonHandles(PersonID("github", "alice")); got != nil {
		t.Fatalf("kept without a key: %v", got)
	}
}
