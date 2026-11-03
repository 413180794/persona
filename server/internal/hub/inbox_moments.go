package hub

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
)

// removed (Feishu Moments, once 头条圈) in the inbox (docs/inbox.md「removed」): what concerns me there — a comment on my
// post, a reply to my comment, an @ in a post or a comment — becomes an item, one per post (a thread: new activity on
// the same post joins the open item). Reactions and new followers are not things to do and stay out. 发送 posts the
// reply as a comment, under the newest comment that concerns me; comments are public to the whole company.

const KindMoment = "moment"

const (
	momentNotices  = 50  // notifications read each collection (one call)
	momentComments = 200 // comments of a post read for its context
	momentShown    = 15  // newest comments put in the context
)

// InboxMoment is a removed item: which post, what happened, and which comment the reply answers.
type InboxMoment struct {
	PostID    string     `json:"post_id"`
	Notice    string     `json:"notice"`              // the newest one's kind: comment | reply | atInPost | atInComment
	Notices   []string   `json:"notices"`             // notification ids in the item, oldest first
	ReplyTo   string     `json:"reply_to,omitempty"`  // the comment 发送 replies to; "" comments on the post itself
	MyPost    bool       `json:"my_post,omitempty"`   // the post is mine
	PostText  string     `json:"post_text,omitempty"` // as the notification quotes it
	Anonymous bool       `json:"anonymous,omitempty"` // the newest actor posted anonymously
	ReadAt    *time.Time `json:"read_at,omitempty"`   // when the comments were last read
}

// momentConcerns: the notifications the inbox takes.
func momentConcerns(n lark.MomentNotice) bool {
	switch n.Kind {
	case "comment", "reply", "atInPost", "atInComment":
		return n.PostID != ""
	}
	return false
}

func (h *Hub) collectMoments(ctx context.Context) ([]*InboxItem, error) {
	ns, err := h.lark.MomentNotices(ctx, momentNotices)
	if err != nil {
		return nil, err
	}
	changed := h.inbox.applyMoments(ns, time.Now())
	if len(changed) == 0 {
		return nil, nil
	}
	me, err := h.larkMe(ctx)
	if err != nil {
		return changed, err
	}
	for _, it := range changed {
		h.readMoment(ctx, it, me.UserID)
	}
	return changed, nil
}

// applyMoments folds the notifications into items, oldest first so an item starts at its first notification. Within
// inboxWindow only; a notification already in an item, or older than the done item of its post, is skipped.
func (b *inbox) applyMoments(ns []lark.MomentNotice, now time.Time) []*InboxItem {
	sort.SliceStable(ns, func(i, j int) bool { return ns[i].At.Before(ns[j].At) })
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[string]bool{}
	for _, it := range b.items {
		if it.Moment != nil {
			for _, id := range it.Moment.Notices {
				seen[id] = true
			}
		}
	}
	var changed []*InboxItem
	touched := map[*InboxItem]bool{}
	for _, n := range ns {
		if !momentConcerns(n) || n.ID == "" || seen[n.ID] || now.Sub(n.At) > inboxWindow {
			continue
		}
		open, done := b.openFor(func(it *InboxItem) bool { return it.Moment != nil && it.Moment.PostID == n.PostID })
		if open == nil && done != nil && !n.At.After(done.UpdatedAt) {
			continue
		}
		it := open
		if it == nil {
			it = &InboxItem{ID: "moment:" + n.PostID + ":" + n.ID, Source: "lark", Kind: KindMoment, Account: "feishu", AccountName: "飞书",
				StartedAt: n.At, Moment: &InboxMoment{PostID: n.PostID}}
			b.items[it.ID] = it
		}
		m := it.Moment
		m.Notices = append(m.Notices, n.ID)
		m.Notice, m.Anonymous = n.Kind, n.Anonymous
		if n.PostText != "" {
			m.PostText = clip(oneLine(n.PostText), 400)
		}
		m.MyPost = m.MyPost || n.Kind == "comment"
		// A reply goes under the comment that concerns me; an @ in the post itself is answered on the post.
		m.ReplyTo = n.CommentID
		it.Title = momentTitle(m.PostText)
		it.Sender = n.ActorName
		if n.Anonymous || it.Sender == "" {
			it.Sender = "匿名"
		}
		text := n.CommentText
		if text == "" {
			text = n.PostText
		}
		it.Preview = clip(oneLine(text), 200)
		it.Count = len(m.Notices)
		it.UpdatedAt = n.At
		it.Read = false
		it.Last = strconv.FormatInt(n.At.UnixMilli(), 10)
		seen[n.ID] = true
		if !touched[it] {
			touched[it] = true
			changed = append(changed, it)
		}
	}
	// Read in Feishu: every notification of an open item is there and none is unread. Not needing me, that settles
	// it (the same rule as chats, inbox.go); needing me, it stays.
	unread := map[string]bool{}
	for _, n := range ns {
		unread[n.ID] = n.Unread
	}
	for _, it := range b.items {
		if it.Moment == nil || !it.open() || it.Read {
			continue
		}
		read := len(it.Moment.Notices) > 0
		for _, id := range it.Moment.Notices {
			if u, ok := unread[id]; !ok || u {
				read = false
				break
			}
		}
		if !read {
			continue
		}
		it.Read = true
		if it.TriagedAt == it.Last && it.Tier != "" && it.Tier != TierAction {
			it.done(now, "read")
		}
		if !touched[it] {
			touched[it] = true
			changed = append(changed, it)
		}
	}
	return changed
}

// momentTitle names an item by its post: the first words of it.
func momentTitle(post string) string {
	t := clip(oneLine(post), 28)
	if t == "" {
		return "removed帖子"
	}
	return t
}

// readMoment reads the post's comments into the item's context, and files the item as done when my comment is the
// newest one after everything in it (I answered on the phone or the web already).
func (h *Hub) readMoment(ctx context.Context, it *InboxItem, myID string) {
	h.inbox.mu.Lock()
	m := *it.Moment
	notices := append([]string(nil), m.Notices...)
	last := it.Last
	h.inbox.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	cs, err := h.lark.MomentCommentList(cctx, m.PostID, momentComments)
	cancel()
	now := time.Now().UTC()
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if it.Moment == nil || it.Moment.PostID != m.PostID || it.Last != last {
		return
	}
	it.Moment.ReadAt = &now
	it.Context = momentContext(it, cs, myID, err, h.userLocation())
	if err == nil && len(notices) > 0 {
		if mine := lastMine(cs, myID); !mine.IsZero() && !mine.Before(it.UpdatedAt) {
			it.done(now, "replied")
		}
	}
}

// lastMine is when my newest comment was written, if it is the newest comment of all.
func lastMine(cs []lark.MomentComment, myID string) time.Time {
	if len(cs) == 0 || myID == "" {
		return time.Time{}
	}
	newest := cs[0]
	for _, c := range cs[1:] {
		if c.At.After(newest.At) {
			newest = c
		}
	}
	if newest.UserID != myID {
		return time.Time{}
	}
	return newest.At
}

// momentContext is the item as text for triage, drafts and 交给 AI: the post, then its newest comments (mine marked,
// the one to reply to marked).
// The comments' times are in loc, the user's time zone.
func momentContext(it *InboxItem, cs []lark.MomentComment, myID string, err error, loc *time.Location) string {
	m := it.Moment
	var b strings.Builder
	whose := "别人的帖子"
	if m.MyPost {
		whose = "我发的帖子"
	}
	fmt.Fprintf(&b, "removed（%s，评论全公司可见）\n帖子：%s\n", whose, orNoText(m.PostText))
	if err != nil {
		fmt.Fprintf(&b, "（评论读不到：%s）\n", clip(oneLine(err.Error()), 120))
		fmt.Fprintf(&b, "最新一条：%s：%s\n", it.Sender, it.Preview)
		return b.String()
	}
	names := map[string]string{}
	for _, c := range cs {
		names[c.ID] = c.UserName
	}
	from := max(0, len(cs)-momentShown)
	if from > 0 {
		fmt.Fprintf(&b, "（前面还有 %d 条评论）\n", from)
	}
	for _, c := range cs[from:] {
		who := c.UserName
		if c.Anonymous || who == "" {
			who = "匿名"
		}
		if c.UserID == myID && myID != "" {
			who = "我"
		}
		to := ""
		if c.ReplyCommentID != "" {
			name := c.ReplyUserName
			if name == "" {
				name = names[c.ReplyCommentID]
			}
			if name != "" {
				to = " 回复 " + name
			}
		}
		mark := ""
		if c.ID == m.ReplyTo && m.ReplyTo != "" {
			mark = "  ← 要回复的这条"
		}
		fmt.Fprintf(&b, "- %s %s%s：%s%s\n", c.At.In(loc).Format("01-02 15:04"), who, to, clip(oneLine(c.Text), inboxText), mark)
	}
	return b.String()
}

func orNoText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（没有文字）"
	}
	return s
}

// replyMoment posts the reply as a comment under ReplyTo (or on the post).
func (h *Hub) replyMoment(ctx context.Context, m InboxMoment, text string) error {
	return h.lark.CommentMoment(ctx, m.PostID, m.ReplyTo, text)
}
