package hub

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// The user's time zone in tests: times are built in UTC and read in it, so a test gives the same result whatever zone
// the machine running it is in (the hub runs in UTC, its user is not).
var testUserZone = mustZone("Asia/Shanghai")

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// What the inbox shows the AI gives times and days in the user's zone, not the hub's: an instant just before UTC
// midnight is the next morning in Shanghai and the same afternoon in Los Angeles.
func TestInboxTimesAreTheUsers(t *testing.T) {
	at := time.Date(2026, 10, 31, 17, 30, 0, 0, time.UTC)
	la := mustZone("America/Los_Angeles")
	msgs := []lark.Message{{ID: "1", SenderName: "张三", Text: "明早的会改到十点", CreatedAt: at}}
	for _, c := range []struct {
		loc   *time.Location
		clock string
	}{{testUserZone, "11-01 01:30"}, {la, "10-31 10:30"}, {time.UTC, "10-31 17:30"}} {
		if got, want := larkContext(lark.Conversation{Name: "张三"}, msgs, c.loc), "["+c.clock+"] 张三：明早的会改到十点"; !strings.Contains(got, want) {
			t.Errorf("%s: larkContext:\n%s\nwant %q", c.loc, got, want)
		}
		it := &InboxItem{Kind: KindMoment, Moment: &InboxMoment{PostID: "p1"}}
		cs := []lark.MomentComment{{ID: "c1", UserName: "张三", Text: "明早的会改到十点", At: at}}
		if got, want := momentContext(it, cs, "me", nil, c.loc), "- "+c.clock+" 张三"; !strings.Contains(got, want) {
			t.Errorf("%s: momentContext:\n%s\nwant %q", c.loc, got, want)
		}
	}

	cell := &lark.BaseCell{Type: "date", Value: []byte(`"` + strconv.FormatInt(at.UnixMilli(), 10) + `"`)}
	for loc, want := range map[*time.Location]string{testUserZone: "2026-11-01", la: "2026-10-31", time.UTC: "2026-10-31"} {
		if got := baseCellText(lark.BaseField{}, cell, loc); got != want {
			t.Errorf("%s: a 多维表格 date = %q, want %q", loc, got, want)
		}
	}
}

// The knowledge base reads the calendar from the user's yesterday to a week after their today, midnight to midnight
// where they are: at 07:00 in Shanghai (23:00 UTC the day before) "today" is Shanghai's.
func TestCalendarWindowIsTheUsersDays(t *testing.T) {
	now := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC) // 11-01 07:00 in Shanghai, 10-31 16:00 in Los Angeles
	for _, c := range []struct {
		loc        *time.Location
		start, end string
	}{
		{testUserZone, "2026-10-30T16:00:00Z", "2026-11-08T16:00:00Z"},
		{mustZone("America/Los_Angeles"), "2026-10-30T07:00:00Z", "2026-11-08T08:00:00Z"}, // DST ends on 11-01 there
		{time.UTC, "2026-10-30T00:00:00Z", "2026-11-08T00:00:00Z"},
	} {
		if s, e := calendarWindow(now, c.loc); s != c.start || e != c.end {
			t.Errorf("%s: window %s – %s, want %s – %s", c.loc, s, e, c.start, c.end)
		}
	}
}
