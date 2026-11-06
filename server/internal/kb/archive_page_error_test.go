package kb

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// pagedSource returns two messages a page and fails the page after position failAfter once.
type pagedSource struct {
	*fakeSource
	failAfter string
	failed    bool
}

func (p *pagedSource) Messages(ctx context.Context, c SourceChat, after string, _ int) ([]SourceMessage, string, bool, error) {
	if after == p.failAfter && !p.failed {
		p.failed = true
		return nil, "", false, errors.New("network")
	}
	return p.fakeSource.Messages(ctx, c, after, 2)
}

// A page that fails after earlier pages were written keeps what they wrote: the next run goes on from there and
// does not write those lines a second time.
func TestArchivePageErrorKeepsProgress(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	y, m, d := time.Now().AddDate(0, 0, -1).Date()
	t0 := time.Date(y, m, d, 12, 0, 0, 0, time.Local)
	var msgs []SourceMessage
	for i := range 4 {
		msgs = append(msgs, msg(i, "张三", "第"+string(rune('1'+i))+"条", t0.Add(time.Duration(i)*time.Minute)))
	}
	// The chat has archived its first message.
	c := &Chat{ID: "lark-f1", Source: "lark", SourceID: "f1", Kind: "group", Name: "群", Cursor: "a", Messages: 1, LastAt: t0}
	if err := st.AppendMessages(c, []Line{{At: t0, Sender: "张三", Text: "第1条"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutChat(c); err != nil {
		t.Fatal(err)
	}
	// Three new messages: the page b..c is written, the page after c fails; the next run the source works again.
	src := &pagedSource{fakeSource: &fakeSource{
		chats: []SourceChat{{ID: "f1", Name: "群", Kind: "group", UpdatedAt: t0.Add(3 * time.Minute)}},
		msgs:  map[string][]SourceMessage{"f1": msgs},
	}, failAfter: "c"}
	e := &Engine{Store: st, Sources: []Source{src}}
	_, _ = e.Archive(context.Background())
	if !src.failed {
		t.Fatal("the page did not fail")
	}
	_, _ = e.Archive(context.Background())

	raw, _ := os.ReadFile(st.Path(KindChat, "lark-f1", "messages", t0.Format("2006-01-02")+".md"))
	for i := range 4 {
		want := "第" + string(rune('1'+i)) + "条"
		if n := strings.Count(string(raw), want); n != 1 {
			t.Fatalf("%s written %d times:\n%s", want, n, raw)
		}
	}
	if c, _ := st.Chat("lark-f1"); c.Cursor != "d" || c.Messages != 4 || c.Pending != 3 {
		t.Fatalf("chat: cursor %q messages %d pending %d", c.Cursor, c.Messages, c.Pending)
	}
}

// A first page that fails writes nothing: the chat is not saved as seen up to the list's time.
func TestArchiveFirstPageErrorSavesNothing(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-time.Hour)
	src := &pagedSource{fakeSource: &fakeSource{
		chats: []SourceChat{{ID: "f1", Name: "群", Kind: "group", UpdatedAt: t0}},
		msgs:  map[string][]SourceMessage{"f1": {msg(0, "张三", "你好", t0)}},
	}, failAfter: ""}
	e := &Engine{Store: st, Sources: []Source{src}}
	_, _ = e.Archive(context.Background())
	if _, ok := st.Chat("lark-f1"); ok {
		t.Fatal("chat saved after its first page failed")
	}
	if changed, _ := e.Archive(context.Background()); len(changed) != 1 {
		t.Fatalf("second run archived %v", changed)
	}
}
