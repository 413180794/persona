package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func readDay(t *testing.T, st *Store, chat, day string) string {
	t.Helper()
	raw, err := os.ReadFile(st.Path(KindChat, chat, "messages", day+".md"))
	if err != nil {
		t.Fatalf("day %s: %v", day, err)
	}
	return string(raw)
}

// An archive written in UTC days (the hub's zone before 2026-11-01) moves into the user's: each line goes to the day
// and the time it was where the user is, once; what is written afterwards lands in the same days; Since still finds
// every line after an instant. The machine's own zone plays no part.
func TestRezoneMovesUTCDaysIntoTheUsers(t *testing.T) {
	shanghai := zone(t, "Asia/Shanghai")
	st, _ := NewStore(t.TempDir())
	if _, err := st.Rezone(time.UTC); err != nil { // as the hub wrote it: UTC
		t.Fatal(err)
	}
	c := &Chat{ID: "lark-a", Name: "项目群"}
	_ = st.PutChat(c)
	at := func(d, hm string) time.Time {
		x, _ := time.ParseInLocation("2006-01-02 15:04", d+" "+hm, time.UTC)
		return x
	}
	if err := st.AppendMessages(c, []Line{
		{At: at("2026-10-31", "09:00"), Sender: "张三", Text: "上午好"},        // 10-31 17:00 in Shanghai
		{At: at("2026-10-31", "17:30"), Sender: "李四", Text: "明早的会\n改到十点"}, // 11-01 01:30 in Shanghai
		{At: at("2026-11-01", "01:00"), Sender: "我", Text: "收到"},          // 11-01 09:00 in Shanghai
	}); err != nil {
		t.Fatal(err)
	}
	if got := st.Days("lark-a"); strings.Join(got, ",") != "2026-10-31,2026-11-01" {
		t.Fatalf("UTC days: %v", got)
	}

	n, err := st.Rezone(shanghai)
	if err != nil || n != 1 {
		t.Fatalf("moved %d, %v", n, err)
	}
	if got := readDay(t, st, "lark-a", "2026-10-31"); got != "# 项目群 · 2026-10-31\n\n- 17:00 张三: 上午好\n" {
		t.Fatalf("10-31 in Shanghai:\n%s", got)
	}
	if got := readDay(t, st, "lark-a", "2026-11-01"); got != "# 项目群 · 2026-11-01\n\n- 01:30 李四: 明早的会\n  改到十点\n- 09:00 我: 收到\n" {
		t.Fatalf("11-01 in Shanghai:\n%s", got)
	}
	if n, err := st.Rezone(shanghai); n != 0 || err != nil {
		t.Fatalf("moved again: %d, %v", n, err)
	}

	// Written now, read now: in Shanghai's days.
	_ = st.AppendMessages(c, []Line{{At: at("2026-11-01", "02:00"), Sender: "张三", Text: "好的"}}) // 10:00 in Shanghai
	if got := readDay(t, st, "lark-a", "2026-11-01"); !strings.HasSuffix(got, "- 09:00 我: 收到\n- 10:00 张三: 好的\n") {
		t.Fatalf("appended:\n%s", got)
	}
	// After 17:30 UTC on 10-31 (01:30 the next day in Shanghai): across the day files, nothing lost or repeated.
	got := st.Since("lark-a", at("2026-10-31", "17:30"), 1<<20)
	if got != "## 2026-11-01\n- 01:30 李四: 明早的会\n  改到十点\n- 09:00 我: 收到\n- 10:00 张三: 好的\n" {
		t.Fatalf("since:\n%s", got)
	}
	if first := st.FirstAt("lark-a"); !first.Equal(at("2026-10-31", "09:00")) {
		t.Fatalf("first at %v", first)
	}
}

// A chat with no day files yet is written in the zone from its first line, and says so.
func TestNewChatIsWrittenInTheZone(t *testing.T) {
	la := zone(t, "America/Los_Angeles")
	st, _ := NewStore(t.TempDir())
	_, _ = st.Rezone(la)
	c := &Chat{ID: "lark-b", Name: "b"}
	at := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC) // 10-31 20:00 in Los Angeles
	_ = st.AppendMessages(c, []Line{{At: at, Sender: "x", Text: "晚上好"}})
	if got := readDay(t, st, "lark-b", "2026-10-31"); !strings.Contains(got, "- 20:00 x: 晚上好") {
		t.Fatalf("day:\n%s", got)
	}
	raw, _ := os.ReadFile(filepath.Join(st.Path(KindChat, "lark-b", "messages"), zoneFile))
	if strings.TrimSpace(string(raw)) != "America/Los_Angeles" {
		t.Fatalf(".zone = %q", raw)
	}
}

// A move cut short is finished, never done twice: before the swap the old days are still the chat's; after it only
// the leftovers go.
func TestRezoneCutShort(t *testing.T) {
	shanghai := zone(t, "Asia/Shanghai")
	at := time.Date(2026, 10, 31, 17, 30, 0, 0, time.UTC) // 11-01 01:30 in Shanghai
	setup := func() (*Store, string) {
		st, _ := NewStore(t.TempDir())
		_, _ = st.Rezone(time.UTC)
		c := &Chat{ID: "lark-c", Name: "c"}
		_ = st.AppendMessages(c, []Line{{At: at, Sender: "x", Text: "y"}})
		return st, st.Path(KindChat, "lark-c", "messages")
	}

	// Cut before the swap: messages.old holds the UTC days, messages is gone, messages.rezone is half written.
	st, dir := setup()
	if err := os.Rename(dir, dir+".old"); err != nil {
		t.Fatal(err)
	}
	_ = writeFile(filepath.Join(dir+".rezone", "2026-11-01.md"), []byte("half"))
	if _, err := st.Rezone(shanghai); err != nil {
		t.Fatal(err)
	}
	if got := readDay(t, st, "lark-c", "2026-11-01"); !strings.Contains(got, "- 01:30 x: y") {
		t.Fatalf("after a cut before the swap:\n%s", got)
	}

	// Cut after the swap: messages is the moved one (it names its zone), messages.old is left over.
	st, dir = setup()
	if _, err := st.Rezone(shanghai); err != nil {
		t.Fatal(err)
	}
	_ = writeFile(filepath.Join(dir+".old", "2026-10-31.md"), []byte("# c · 2026-10-31\n\n- 17:30 x: y\n"))
	st.settled = "" // a new process: it walks again
	if n, err := st.Rezone(shanghai); n != 0 || err != nil {
		t.Fatalf("moved again: %d, %v", n, err)
	}
	if _, err := os.Stat(dir + ".old"); !os.IsNotExist(err) {
		t.Fatal("the leftover stays")
	}
	if got := st.Days("lark-c"); strings.Join(got, ",") != "2026-11-01" {
		t.Fatalf("days %v", got)
	}
}

// A project's timeline moves too (its header names the zone), and new lines follow it.
func TestRezoneTimeline(t *testing.T) {
	shanghai := zone(t, "Asia/Shanghai")
	st, _ := NewStore(t.TempDir())
	_, _ = st.Rezone(time.UTC)
	_ = st.PutProject(&Project{Slug: "p", Name: "P"})
	_ = st.AppendLog("p", time.Date(2026, 10, 31, 17, 30, 0, 0, time.UTC), "lark", "定了周五灰度")
	if n, err := st.Rezone(shanghai); n != 1 || err != nil {
		t.Fatalf("moved %d, %v", n, err)
	}
	_ = st.AppendLog("p", time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC), "lark", "提前到周四")
	raw, _ := os.ReadFile(st.Path(KindProject, "p", "log.md"))
	want := "# 时间线 · Asia/Shanghai\n\n- 2026-11-01 01:30 · lark · 定了周五灰度\n- 2026-11-01 10:00 · lark · 提前到周四\n"
	if string(raw) != want {
		t.Fatalf("log.md:\n%s\nwant:\n%s", raw, want)
	}
}
