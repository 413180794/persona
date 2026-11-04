package hub

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
)

// A connector's program writes a feed's public posts into the knowledge base's post archive (moments/ in the data
// dir, the name it had when the hub's own crawler was the only writer) and reads which posts' comments to read again:
// connector.KnowledgeBase, docs/knowledge.md「外部帖子」. The archive is the hub's to keep: the index, pruning past
// the text retention, and finding each project's posts (kb.LinkMoments) stay here whoever writes the posts.

// kbFor is the knowledge base as connector id's program sees it.
func (h *Hub) kbFor(id string) connector.KnowledgeBase { return kbPosts{h: h, id: id} }

type kbPosts struct {
	h  *Hub
	id string
}

var errKBOff = errors.New("这台 hub 没开知识库")

// engine is the knowledge base, when this connector may write to it now.
func (k kbPosts) engine() (*kb.Engine, error) {
	e := k.h.kb.eng
	if e == nil {
		return nil, errKBOff
	}
	if k.h.connectors != nil && !k.h.connectors.Feeds(k.id, connector.FeedArchive) {
		return nil, errors.New("用户关了这个连接器的「进知识库」")
	}
	return e, nil
}

// sources are the names the connector's conversations are filed under: its id, then its manifest's ArchiveSources.
func (k kbPosts) sources() []string {
	out := []string{k.id}
	if k.h.connectors != nil {
		if c := k.h.connectors.Get(k.id); c != nil {
			out = append(out, c.Manifest().ArchiveSources...)
		}
	}
	return out
}

func (k kbPosts) PutPosts(ctx context.Context, posts []connector.PostPut) ([]string, error) {
	e, err := k.engine()
	if err != nil {
		return nil, err
	}
	if len(posts) > connector.PostsPerPut {
		return nil, errors.New("too many posts in one call")
	}
	st := e.Store
	sources := k.sources()
	now := time.Now().UTC()
	out := make([]string, len(posts))
	for i, in := range posts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p := in.Post
		if strings.TrimSpace(p.ID) == "" || len(in.Comments) > connector.PostCommentsMost {
			out[i] = connector.PostRefused
			continue
		}
		old, ok := st.MomentPost(p.ID)
		if ok && old.Source != "" && old.Source != k.id {
			out[i] = connector.PostRefused
			continue
		}
		m := &kb.MomentPost{ID: p.ID, Author: p.Author, AuthorID: p.AuthorID, Anonymous: p.Anonymous, Categories: p.Categories,
			Text: clipBytes(p.Text, connector.PostTextMost), Images: p.Images, Reactions: p.Reactions, CommentCount: p.CommentCount,
			At: p.At.UTC(), SeenAt: now, CommentsOf: -1, Mine: p.Mine, Source: k.id, Feed: p.Feed}
		// It concerns the user when the connector archives it as a conversation too (one of its names, the post's id).
		m.Concerns = slices.ContainsFunc(sources, func(s string) bool {
			_, ok := st.Chat(kb.ChatID(s, p.ID))
			return ok
		})
		out[i] = connector.PostNew
		if ok {
			m.CommentsOf, m.CommentsAt = old.CommentsOf, old.CommentsAt
			m.Concerns = m.Concerns || old.Concerns
			out[i] = connector.PostSame
			if old.CommentCount != p.CommentCount {
				out[i] = connector.PostChanged
			}
		}
		comments := []kb.MomentComment(nil)
		switch {
		case in.Comments != nil:
			comments = make([]kb.MomentComment, 0, len(in.Comments))
			for _, c := range in.Comments {
				comments = append(comments, kb.MomentComment{Author: c.Author, ReplyTo: c.ReplyTo, Text: clipBytes(c.Text, connector.PostTextMost), At: c.At.UTC()})
			}
			m.CommentsOf, m.CommentsAt = p.CommentCount, now
		case p.CommentCount == 0 && m.CommentsOf != 0: // nothing to read: settled at once
			comments = []kb.MomentComment{}
			m.CommentsOf, m.CommentsAt = 0, now
		}
		if err := st.PutMomentPost(m, comments); err != nil {
			return nil, err
		}
	}
	k.h.kb.postsDirty.Store(true)
	return out, nil
}

func (k kbPosts) PutComments(ctx context.Context, id string, count int, in []connector.PostComment) error {
	e, err := k.engine()
	if err != nil {
		return err
	}
	if len(in) > connector.PostCommentsMost {
		return errors.New("too many comments")
	}
	p, ok := e.Store.MomentPost(id)
	if !ok || p.Source != "" && p.Source != k.id {
		return errors.New("no such post of this connector's in the archive")
	}
	comments := make([]kb.MomentComment, 0, len(in))
	for _, c := range in {
		comments = append(comments, kb.MomentComment{Author: c.Author, ReplyTo: c.ReplyTo, Text: clipBytes(c.Text, connector.PostTextMost), At: c.At.UTC()})
	}
	p.Source, p.CommentCount, p.CommentsOf, p.CommentsAt = k.id, count, count, time.Now().UTC()
	if err := e.Store.PutMomentPost(p, comments); err != nil {
		return err
	}
	k.h.kb.postsDirty.Store(true)
	return nil
}

func (k kbPosts) PendingPosts(ctx context.Context, limit int) ([]connector.PendingPost, error) {
	e, err := k.engine()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > connector.PendingPostsMost {
		limit = connector.PendingPostsMost
	}
	cutoff := time.Now().Add(-kbTextRetention(e))
	var todo []*kb.MomentPost
	for _, p := range e.Store.MomentPosts() {
		if (p.Source == "" || p.Source == k.id) && !p.At.Before(cutoff) && p.CommentCount > 0 && p.CommentsOf != p.CommentCount {
			todo = append(todo, p)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].At.After(todo[j].At) })
	out := []connector.PendingPost{}
	for _, p := range todo {
		if len(out) >= limit {
			break
		}
		out = append(out, connector.PendingPost{ID: p.ID, CommentCount: p.CommentCount, At: p.At})
	}
	return out, nil
}

func (k kbPosts) ArchivedChats(ctx context.Context, source string) ([]connector.ArchivedChat, error) {
	e, err := k.engine()
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = k.id
	}
	if !slices.Contains(k.sources(), source) {
		return nil, errors.New("not one of the connector's names: " + source)
	}
	out := []connector.ArchivedChat{}
	for _, c := range e.Store.Chats() {
		if c.Source == source {
			out = append(out, connector.ArchivedChat{ID: c.SourceID, Name: c.Name, Cursor: c.Cursor, LastAt: c.LastAt, Messages: c.Messages})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// kbTextRetention is how long the knowledge base keeps text (kb.text_days, a year by default).
func kbTextRetention(e *kb.Engine) time.Duration {
	days := e.TextDays
	if days <= 0 {
		days = 365
	}
	return time.Duration(days) * 24 * time.Hour
}

// kbPostsUpkeep rewrites the post archive's index after posts came in, and prunes it past the text retention once
// a day (kbLoop; the hub's own crawler did both itself).
func (h *Hub) kbPostsUpkeep(daily bool) {
	e := h.kb.eng
	if daily {
		if n := e.Store.PruneMoments(time.Now().Add(-kbTextRetention(e))); n > 0 {
			h.logf("kb: pruned %d post(s) past the text retention", n)
			h.kb.postsDirty.Store(true)
		}
	}
	if h.kb.postsDirty.Swap(false) {
		if _, err := e.Store.WriteMomentIndex(); err != nil {
			h.logf("kb: posts index: %v", err)
		}
	}
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
