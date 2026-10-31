package kb

import (
	"strings"
	"testing"
	"time"
)

func TestFindRanksEntriesAndMatchesLocateMessages(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: st}
	day := func(d string, h int) time.Time {
		at, _ := time.ParseInLocation("2006-01-02 15:04", d+" 00:00", time.Local)
		return at.Add(time.Duration(h) * time.Hour)
	}
	low := &Chat{ID: "lark-low", Name: "闲聊群", Kind: "group", Tagging: Tagging{Importance: "P3", Relevance: 10}}
	high := &Chat{ID: "lark-high", Name: "发布群", Kind: "group", Tagging: Tagging{Importance: "P1", Relevance: 80}}
	named := &Chat{ID: "lark-named", Name: "周报提醒", Kind: "bot", Tagging: Tagging{Importance: "P3"}}
	for _, c := range []*Chat{low, high, named} {
		if err := st.PutChat(c); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.AppendMessages(low, []Line{{At: day("2026-10-01", 9), Sender: "甲", Text: "今天写周报了吗"}})
	_ = st.AppendMessages(high, []Line{
		{At: day("2026-10-20", 9), Sender: "乙", Text: "早"},
		{At: day("2026-10-20", 10), Sender: "丙", Text: "周报发出来了\n第二行"},
		{At: day("2026-10-20", 11), Sender: "乙", Text: "收到"},
		{At: day("2026-10-21", 9), Sender: "乙", Text: "下周周报我来写"},
	})

	res := e.Find("周报", 10)
	if len(res) != 3 {
		t.Fatalf("results: %+v", res)
	}
	// The name match first, then by importance.
	if res[0].Entry.ID != "lark-named" || !res[0].InName || res[1].Entry.ID != "lark-high" || res[2].Entry.ID != "lark-low" {
		t.Fatalf("order: %s %s %s", res[0].Entry.ID, res[1].Entry.ID, res[2].Entry.ID)
	}
	if res[1].Count != 2 || len(res[1].Hits) != 2 || !strings.Contains(res[1].Hits[0].Path, "2026-10-21") {
		t.Fatalf("high: %+v", res[1])
	}

	m := e.Matches(KindChat, "lark-high", "周报", 10)
	if len(m) != 2 || m[0].Day != "2026-10-21" || m[1].Day != "2026-10-20" {
		t.Fatalf("matches: %+v", m)
	}
	// The message with its continuation line, and its neighbours.
	if !strings.Contains(m[1].Line, "第二行") || len(m[1].Before) != 1 || len(m[1].After) != 1 || !strings.Contains(m[1].After[0], "收到") {
		t.Fatalf("context: %+v", m[1])
	}

	msgs, prev, next, err := e.Day("lark-high", "2026-10-20")
	if err != nil || len(msgs) != 3 || prev != "" || next != "2026-10-21" {
		t.Fatalf("day: %+v %q %q %v", msgs, prev, next, err)
	}
	found := false
	for _, x := range msgs {
		if x.N == m[1].N {
			found = strings.Contains(x.Text, "周报发出来了")
		}
	}
	if !found {
		t.Fatalf("the match's line %d is not a message of the day: %+v", m[1].N, msgs)
	}
	if _, _, _, err := e.Day("lark-high", "../meta"); err != ErrNotFound {
		t.Fatalf("bad day: %v", err)
	}
}
