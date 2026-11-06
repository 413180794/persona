package kb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// A page whose day file cannot be written keeps what the earlier pages wrote, and the failed page is read again on
// the next run: its messages are written once, not skipped and not twice.
func TestArchiveAppendErrorKeepsProgress(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	y, m, d := time.Now().AddDate(0, 0, -2).Date()
	t0 := time.Date(y, m, d, 12, 0, 0, 0, time.Local)
	day1 := t0.Add(24 * time.Hour)
	msgs := []SourceMessage{
		msg(0, "张三", "第1条", t0),
		msg(1, "张三", "第2条", t0.Add(time.Minute)),
		msg(2, "张三", "第3条", t0.Add(2*time.Minute)),
		msg(3, "张三", "第4条", day1),
	}
	// The chat has archived its first message.
	c := &Chat{ID: "lark-f1", Source: "lark", SourceID: "f1", Kind: "group", Name: "群", Cursor: "a", Messages: 1, LastAt: t0}
	if err := st.AppendMessages(c, []Line{{At: t0, Sender: "张三", Text: "第1条"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutChat(c); err != nil {
		t.Fatal(err)
	}
	// The page b..c goes to t0's day file; the page d goes to the next day's, which is a directory and cannot be
	// written. Two messages a page (pagedSource never fails here).
	blocked := st.Path(KindChat, "lark-f1", "messages", day1.Format("2006-01-02")+".md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	src := &pagedSource{fakeSource: &fakeSource{
		chats: []SourceChat{{ID: "f1", Name: "群", Kind: "group", UpdatedAt: day1}},
		msgs:  map[string][]SourceMessage{"f1": msgs},
	}, failAfter: "never"}
	e := &Engine{Store: st, Sources: []Source{src}}
	_, _ = e.Archive(context.Background())
	if c, _ := st.Chat("lark-f1"); c.Cursor != "c" || c.Messages != 3 || !c.LastAt.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("after the failed write: cursor %q messages %d last %v", c.Cursor, c.Messages, c.LastAt)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	_, _ = e.Archive(context.Background())

	raw0, _ := os.ReadFile(st.Path(KindChat, "lark-f1", "messages", t0.Format("2006-01-02")+".md"))
	raw1, _ := os.ReadFile(blocked)
	raw := string(raw0) + string(raw1)
	for i := range 4 {
		want := "第" + string(rune('1'+i)) + "条"
		if n := strings.Count(raw, want); n != 1 {
			t.Fatalf("%s written %d times:\n%s", want, n, raw)
		}
	}
	if c, _ := st.Chat("lark-f1"); c.Cursor != "d" || c.Messages != 4 || c.Pending != 3 {
		t.Fatalf("chat: cursor %q messages %d pending %d", c.Cursor, c.Messages, c.Pending)
	}
}
