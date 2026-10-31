package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
)

// The removed crawler (docs/knowledge.md「removed全量」): every so often, page through each feed tab (推荐 and every
// category) and keep each post as text in <data>/moments, then read the comments of posts whose comment count moved.
// Archive only (no model calls). The feeds are ordered by heat, not time, so a tab is read until a page brings
// nothing new or changed, or the page budget runs out: over time this comes close to everything without promising it.

// MomentsCrawl is hub.json's kb.moments. Off unless enabled: it reads the company's whole feed with your account.
type MomentsCrawl struct {
	Enabled          bool `json:"enabled"`
	IntervalMinutes  int  `json:"interval_minutes,omitempty"`   // between rounds (30)
	PagesPerTab      int  `json:"pages_per_tab,omitempty"`      // feed pages (50 posts) a tab is read at most per round (10)
	CommentsPerRound int  `json:"comments_per_round,omitempty"` // posts whose comments are read per round (150)
	DelayMS          int  `json:"delay_ms,omitempty"`           // between two requests (300)
}

func (c MomentsCrawl) interval() time.Duration {
	return time.Duration(or(c.IntervalMinutes, 30)) * time.Minute
}
func (c MomentsCrawl) delay() time.Duration {
	return time.Duration(or(c.DelayMS, 300)) * time.Millisecond
}

func or(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

// MomentsRound is what one round did (logged, and kept in moments/stats.json).
type MomentsRound struct {
	At           time.Time     `json:"at"`
	Took         time.Duration `json:"took_ns"`
	Tabs         int           `json:"tabs"`
	Pages        int           `json:"pages"`
	Requests     int           `json:"requests"`
	Seen         int           `json:"seen"`     // distinct posts in the feeds this round
	New          int           `json:"new"`      // first time archived
	Changed      int           `json:"changed"`  // comment count moved
	Comments     int           `json:"comments"` // posts whose comments were read
	CommentsLeft int           `json:"comments_left"`
	Total        int           `json:"total"` // posts in the archive
	Bytes        int64         `json:"bytes"` // size of moments/
	Error        string        `json:"error,omitempty"`
}

func (h *Hub) momentsCrawlLoop(ctx context.Context, st *kb.Store, cfg MomentsCrawl, textDays int) {
	var lastPrune time.Time
	for {
		r := h.crawlMoments(ctx, st, cfg, textDays)
		h.logf("kb: removed round: %d tab(s) %d page(s) %d request(s) in %s; %d seen, %d new, %d changed, comments of %d (%d left); %d archived, %.1f MB%s",
			r.Tabs, r.Pages, r.Requests, r.Took.Round(time.Second), r.Seen, r.New, r.Changed, r.Comments, r.CommentsLeft, r.Total,
			float64(r.Bytes)/1e6, map[bool]string{true: "; " + r.Error, false: ""}[r.Error != ""])
		if time.Since(lastPrune) > 24*time.Hour {
			lastPrune = time.Now()
			if n := st.PruneMoments(time.Now().AddDate(0, 0, -textDays)); n > 0 {
				h.logf("kb: removed: pruned %d post(s) older than %d days", n, textDays)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.interval()):
		}
	}
}

func (h *Hub) crawlMoments(ctx context.Context, st *kb.Store, cfg MomentsCrawl, textDays int) (r MomentsRound) {
	start := time.Now()
	r.At = start.UTC()
	defer func() {
		r.Took = time.Since(start)
		r.Total, _ = st.WriteMomentIndex()
		r.Bytes = dirSize(filepath.Join(st.Dir, "moments"))
		raw, _ := json.MarshalIndent(r, "", "  ")
		_ = os.WriteFile(filepath.Join(st.Dir, "moments", "stats.json"), append(raw, '\n'), 0o600)
	}()
	wait := func() bool {
		r.Requests++
		select {
		case <-ctx.Done():
			return false
		case <-time.After(cfg.delay()):
			return true
		}
	}
	me, _ := h.larkMe(ctx)
	cutoff := time.Now().AddDate(0, 0, -textDays)
	tabs, err := h.lark.MomentTabs(ctx)
	if err != nil {
		r.Error = "tabs: " + err.Error()
		return r
	}
	seen := map[string]bool{}
	for _, tab := range tabs {
		if tab.ID == "" {
			continue
		}
		r.Tabs++
		token := ""
		for page := 0; page < or(cfg.PagesPerTab, 10); page++ {
			if !wait() {
				return r
			}
			posts, next, err := h.lark.MomentFeed(ctx, tab.ID, token, 50)
			if err != nil {
				r.Error = tab.Name + ": " + err.Error()
				if rateLimited(err) {
					return r // back off until the next round
				}
				break
			}
			r.Pages++
			fresh := 0
			for _, p := range posts {
				if p.ID == "" || p.At.Before(cutoff) {
					continue
				}
				if seen[p.ID] { // in another tab already this round: its first sighting stands
					continue
				}
				seen[p.ID] = true
				r.Seen++
				switch h.archiveMomentMeta(st, p, me.UserID) {
				case "new":
					r.New++
					fresh++
				case "changed":
					r.Changed++
					fresh++
				}
			}
			// Heat order: a page with nothing new or changed means the rest of this tab is what we have already
			// (posts already met in another tab this round count as nothing new).
			if next == "" || next == token || len(posts) == 0 || fresh == 0 {
				break
			}
			token = next
		}
	}
	// Comments of the posts whose count moved (newest first), within the round's budget.
	var todo []*kb.MomentPost
	for _, p := range st.MomentPosts() {
		if p.CommentsOf != p.CommentCount {
			todo = append(todo, p)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].At.After(todo[j].At) })
	for i, p := range todo {
		if i >= or(cfg.CommentsPerRound, 150) || ctx.Err() != nil {
			r.CommentsLeft = len(todo) - i
			break
		}
		if p.CommentCount == 0 {
			p.CommentsOf, p.CommentsAt = 0, time.Now().UTC()
			_ = st.PutMomentPost(p, []kb.MomentComment{})
			continue
		}
		if !wait() {
			r.CommentsLeft = len(todo) - i
			break
		}
		cs, err := h.lark.MomentCommentList(ctx, p.ID, 1000)
		if err != nil {
			r.Error = "comments: " + err.Error()
			if rateLimited(err) {
				r.CommentsLeft = len(todo) - i
				break
			}
			continue
		}
		out := make([]kb.MomentComment, 0, len(cs))
		for _, c := range cs {
			who := c.UserName
			if c.Anonymous {
				who = "匿名"
			}
			if me.UserID != "" && c.UserID == me.UserID {
				who = "我"
			}
			out = append(out, kb.MomentComment{Author: who, ReplyTo: c.ReplyUserName, Text: c.Text, At: c.At})
		}
		p.CommentsOf, p.CommentsAt = p.CommentCount, time.Now().UTC()
		if err := st.PutMomentPost(p, out); err == nil {
			r.Comments++
		}
	}
	return r
}

// archiveMomentMeta records a post seen in a feed: "new", "changed" (its comment count moved) or "".
func (h *Hub) archiveMomentMeta(st *kb.Store, p lark.MomentFeedPost, myID string) string {
	old, ok := st.MomentPost(p.ID)
	m := &kb.MomentPost{ID: p.ID, Author: p.UserName, AuthorID: p.UserID, Anonymous: p.Anonymous, Categories: p.Categories,
		Text: p.Text, Images: p.Images, Reactions: p.Reactions, CommentCount: p.CommentCount, At: p.At.UTC(), SeenAt: time.Now().UTC(),
		CommentsOf: -1, Mine: myID != "" && p.UserID == myID}
	if _, err := os.Stat(st.Path(kb.KindChat, kb.ChatID("moments", p.ID), "meta.json")); err == nil {
		m.Concerns = true
	}
	status := "new"
	if ok {
		m.CommentsOf, m.CommentsAt = old.CommentsOf, old.CommentsAt
		m.Concerns = m.Concerns || old.Concerns
		status = ""
		if old.CommentCount != p.CommentCount {
			status = "changed"
		}
	}
	if err := st.PutMomentPost(m, nil); err != nil {
		h.logf("kb: removed %s: %v", p.ID, err)
	}
	return status
}

func rateLimited(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") || strings.Contains(s, "rate") || strings.Contains(s, "too many")
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			n += fi.Size()
		}
		return nil
	})
	return n
}
