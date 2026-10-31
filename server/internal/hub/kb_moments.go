package hub

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
)

// momentsSource puts the removed posts I am part of into the knowledge base (docs/knowledge.md): my own posts and the
// posts where someone commented on, replied to or @ed me. A post is a chat (moments-<postId>), its text the first
// message and its comments the rest, so they are archived, summarized and tagged like any chat.
type momentsSource struct {
	c *lark.Client

	mu    sync.Mutex
	posts map[string]lark.MomentPost // what Chats saw, for the post's own text in Messages
	me    string                     // my user id, read once: my post and comments are written as 我
}

func (s *momentsSource) myID(ctx context.Context) string {
	s.mu.Lock()
	id := s.me
	s.mu.Unlock()
	if id != "" {
		return id
	}
	if me, err := s.c.Me(ctx); err == nil && me.UserID != "" {
		s.mu.Lock()
		s.me = me.UserID
		s.mu.Unlock()
		return me.UserID
	}
	return ""
}

func (s *momentsSource) Name() string { return "moments" }

func (s *momentsSource) Chats(ctx context.Context, limit int) ([]kb.SourceChat, error) {
	posts := map[string]lark.MomentPost{}
	active := map[string]time.Time{}
	mine, err := s.c.MyMomentPosts(ctx, 20)
	if err != nil {
		return nil, err
	}
	for _, p := range mine {
		posts[p.ID], active[p.ID] = p, p.At
	}
	ns, err := s.c.MomentNotices(ctx, momentNotices)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		if !momentConcerns(n) {
			continue
		}
		if _, ok := posts[n.PostID]; !ok {
			posts[n.PostID] = lark.MomentPost{ID: n.PostID, Text: n.PostText, At: n.At}
		}
		if n.At.After(active[n.PostID]) {
			active[n.PostID] = n.At
		}
	}
	s.mu.Lock()
	s.posts = posts
	s.mu.Unlock()
	out := make([]kb.SourceChat, 0, len(posts))
	for id, p := range posts {
		out = append(out, kb.SourceChat{ID: id, Name: "removed · " + momentTitle(p.Text), Kind: "moment", UpdatedAt: active[id]})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Messages: the post (when it is new to the archive) and its comments newer than after (milliseconds), oldest first.
func (s *momentsSource) Messages(ctx context.Context, c kb.SourceChat, after string, limit int) ([]kb.SourceMessage, string, bool, error) {
	since, _ := strconv.ParseInt(after, 10, 64)
	me := s.myID(ctx)
	var out []kb.SourceMessage
	s.mu.Lock()
	p, ok := s.posts[c.ID]
	s.mu.Unlock()
	if ok && p.Text != "" && p.At.UnixMilli() > since {
		name := p.UserName
		if name == "" {
			name = "帖子"
		}
		out = append(out, kb.SourceMessage{ID: "post-" + p.ID, Position: strconv.FormatInt(p.At.UnixMilli(), 10), SenderID: p.UserID,
			SenderName: name, Mine: me != "" && p.UserID == me, Type: "TEXT", Text: "（发帖）" + p.Text, At: p.At})
	}
	cs, err := s.c.MomentCommentList(ctx, c.ID, max(limit, momentComments))
	if err != nil {
		return nil, "", false, err
	}
	for _, cm := range cs {
		if cm.At.UnixMilli() <= since || cm.Text == "" {
			continue
		}
		name := cm.UserName
		if cm.Anonymous || name == "" {
			name = "匿名"
		}
		text := cm.Text
		if cm.ReplyUserName != "" {
			text = "（回复 " + cm.ReplyUserName + "）" + text
		}
		out = append(out, kb.SourceMessage{ID: cm.ID, Position: strconv.FormatInt(cm.At.UnixMilli(), 10), SenderID: cm.UserID,
			SenderName: name, Mine: me != "" && cm.UserID == me, Type: "TEXT", Text: text, At: cm.At})
	}
	newer := ""
	if len(out) > 0 {
		newer = out[len(out)-1].Position
	}
	return out, newer, false, nil
}

func (s *momentsSource) Download(context.Context, string, string) error {
	return errors.New("removed items carry no media in the archive")
}

// Older has nothing: the first archive already reads all of a post's comments. Being a Backfiller, the post is marked
// backfilled at once instead of holding a backfill slot every run.
func (s *momentsSource) Older(context.Context, kb.SourceChat, string, int) ([]kb.SourceMessage, string, bool, error) {
	return nil, "", false, nil
}
