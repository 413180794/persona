package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

func notice(id, kind, post, comment, actor, text string, at time.Time, unread bool) lark.MomentNotice {
	return lark.MomentNotice{ID: id, Kind: kind, PostID: post, CommentID: comment, ActorName: actor, CommentText: text,
		PostText: "换电脑怎么迁移数据", At: at, Unread: unread}
}

func TestInboxMomentsOneItemPerPost(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	b := newInbox()
	ns := []lark.MomentNotice{ // newest first, as lark-connector returns them
		notice("n3", "reply", "p1", "c3", "王五", "那用什么线", now.Add(-time.Minute), true),
		notice("n4", "postReaction", "p1", "", "赵六", "", now.Add(-30*time.Second), true),
		notice("n2", "comment", "p1", "c2", "李四", "我也想知道", now.Add(-time.Hour), true),
		notice("n1", "atInPost", "p2", "", "张三", "", now.Add(-2*time.Hour), true),
		notice("n0", "comment", "p3", "c0", "老王", "好久以前", now.Add(-10*24*time.Hour), true),
	}
	changed := b.applyMoments(ns, now)
	if len(changed) != 2 {
		t.Fatalf("changed = %d, want p1 and p2 (the reaction and the old one stay out)", len(changed))
	}
	var p1, p2 *InboxItem
	for _, it := range changed {
		switch it.Moment.PostID {
		case "p1":
			p1 = it
		case "p2":
			p2 = it
		}
	}
	if p1 == nil || p2 == nil {
		t.Fatalf("items = %+v", changed)
	}
	if p1.ID != "moment:p1:n2" || p1.Kind != KindMoment || p1.Count != 2 || p1.Sender != "王五" || p1.Preview != "那用什么线" ||
		p1.Moment.ReplyTo != "c3" || !p1.Moment.MyPost || p1.Title != "换电脑怎么迁移数据" {
		t.Fatalf("p1 = %+v %+v", p1, p1.Moment)
	}
	if p2.Moment.ReplyTo != "" || p2.Moment.Notice != "atInPost" || ruleTier(p2) != TierAction || ruleTier(p1) != TierFYI {
		t.Fatalf("p2 = %+v, tiers %s / %s", p2.Moment, ruleTier(p2), ruleTier(p1))
	}
	// The same notifications again: nothing new.
	if ch := b.applyMoments(ns, now); len(ch) != 0 {
		t.Fatalf("re-applied: %d", len(ch))
	}
	// Read in Feishu: the FYI item settles once triaged on its content; the @ that needs me stays.
	p1.Tier, p1.TriagedAt = TierFYI, p1.Last
	p2.Tier, p2.TriagedAt = TierAction, p2.Last
	for i := range ns {
		ns[i].Unread = false
	}
	b.applyMoments(ns, now)
	if !p1.Read || p1.open() || p1.DoneBy != "read" {
		t.Fatalf("p1 read elsewhere: read %v done %v by %q", p1.Read, p1.DoneAt, p1.DoneBy)
	}
	if !p2.Read || !p2.open() {
		t.Fatalf("p2 needs me and must stay: %+v", p2)
	}
	// Activity after the item was done starts a new one.
	later := append([]lark.MomentNotice{notice("n5", "comment", "p1", "c5", "钱七", "后来呢", now.Add(time.Minute), true)}, ns...)
	ch := b.applyMoments(later, now.Add(2*time.Minute))
	if len(ch) != 1 || ch[0].ID != "moment:p1:n5" || ch[0].Count != 1 {
		t.Fatalf("new activity: %+v", ch)
	}
}

func TestMomentContextAndMyReply(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	it := &InboxItem{Kind: KindMoment, Sender: "王五", Preview: "那用什么线", UpdatedAt: t0.Add(2 * time.Minute),
		Moment: &InboxMoment{PostID: "p1", ReplyTo: "c2", MyPost: true, PostText: "换电脑怎么迁移数据"}}
	cs := []lark.MomentComment{
		{ID: "c1", UserID: "me", UserName: "我自己", Text: "用雷电线", At: t0},
		{ID: "c2", UserID: "u5", UserName: "王五", ReplyCommentID: "c1", Text: "那用什么线", At: t0.Add(2 * time.Minute)},
	}
	ctx := momentContext(it, cs, "me", nil, testUserZone)
	for _, want := range []string{"我发的帖子", "全公司可见", "换电脑怎么迁移数据", "11-01 18:00 我：用雷电线", "王五 回复 我自己：那用什么线  ← 要回复的这条"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context lacks %q:\n%s", want, ctx)
		}
	}
	if !lastMine(cs, "me").IsZero() {
		t.Fatal("the newest comment is not mine")
	}
	cs = append(cs, lark.MomentComment{ID: "c3", UserID: "me", Text: "type-c 就行", At: t0.Add(3 * time.Minute)})
	if got := lastMine(cs, "me"); !got.Equal(t0.Add(3 * time.Minute)) {
		t.Fatalf("lastMine = %v", got)
	}
}
