package hub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/lark"
)

// The removed crawler (docs/knowledge.md「removed全量」): every so often, page through each feed tab (推荐 and every
// category) and keep each post as text in <data>/moments, then read the comments of posts whose comment count moved.
// Archive only (no model calls). The feeds are ordered by heat, not time, so a tab is read until a page brings
// nothing new or changed, or the page budget runs out: over time this comes close to everything without promising it.

// MomentsCrawl is hub.json's kb.moments. Off unless enabled: it reads the company's whole feed with your account.
type MomentsCrawl struct {
	Enabled          bool `json:"enabled"`
	IntervalMinutes  int  `json:"interval_minutes,omitempty"`   // between rounds (30)
	HeadPages        int  `json:"head_pages,omitempty"`         // pages from the top of a tab read for what is new (3)
	PagesPerTab      int  `json:"pages_per_tab,omitempty"`      // pages further down a tab read per round, from where the last stopped (10)
	CommentsPerRound int  `json:"comments_per_round,omitempty"` // posts whose comments are read per round (150)
	DelayMS          int  `json:"delay_ms,omitempty"`           // between two requests (300)
	// MaxDays: posts older than this are not wanted (2026-11-01: a year is enough, and reading further only loads
	// lark-connector and Feishu). A deep pass stops at a page that is all older; their comments are not read. (365, and
	// never more than kb.text_days.)
	MaxDays int `json:"max_days,omitempty"`
}

// maxAge is how far back posts are kept and read.
func (c MomentsCrawl) maxAge(textDays int) time.Duration {
	days := or(c.MaxDays, 365)
	if textDays > 0 && textDays < days {
		days = textDays
	}
	return time.Duration(days) * 24 * time.Hour
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

// momentsTab is how far down a tab the deep pass has read.
type momentsTab struct {
	Token string `json:"token,omitempty"`
	Done  bool   `json:"done,omitempty"` // reached the end of the tab
}

// MomentsRound is what one round did (logged, and kept in moments/stats.json).
type MomentsRound struct {
	At           time.Time     `json:"at"`
	Took         time.Duration `json:"took_ns"`
	Tabs         int           `json:"tabs"`
	Pages        int           `json:"pages"`
	Requests     int           `json:"requests"`
	Seen         int           `json:"seen"`                 // distinct posts in the feeds this round
	Old          int           `json:"old"`                  // posts past max_days in the pages read (skipped)
	TabsEnded    int           `json:"tabs_ended,omitempty"` // tabs whose deep pass reached posts past max_days this round
	New          int           `json:"new"`                  // first time archived
	Changed      int           `json:"changed"`              // comment count moved
	Comments     int           `json:"comments"`             // posts whose comments were read
	CommentsLeft int           `json:"comments_left"`
	Total        int           `json:"total"` // posts in the archive
	Bytes        int64         `json:"bytes"` // size of moments/
	Error        string        `json:"error,omitempty"`
}

func (h *Hub) momentsCrawlLoop(ctx context.Context, st *kb.Store, cfg MomentsCrawl, textDays int) {
	var lastPrune time.Time
	for {
		// The center crawls for the group; a plugin that took Feishu over crawls through posts.put (kb_posts.go).
		if h.isMember() || h.larkByPlugin() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(cfg.interval()):
			}
			continue
		}
		r := h.crawlMoments(ctx, st, cfg, textDays)
		h.logf("kb: removed round: %d tab(s) %d page(s) %d request(s) in %s; %d seen, %d new, %d changed, %d past max_days (%d tab(s) ended there), comments of %d (%d left); %d archived, %.1f MB%s",
			r.Tabs, r.Pages, r.Requests, r.Took.Round(time.Second), r.Seen, r.New, r.Changed, r.Old, r.TabsEnded, r.Comments, r.CommentsLeft, r.Total,
			float64(r.Bytes)/1e6, map[bool]string{true: "; " + r.Error, false: ""}[r.Error != ""])
		if time.Since(lastPrune) > 24*time.Hour {
			lastPrune = time.Now()
			if n := st.PruneMoments(time.Now().Add(-cfg.maxAge(textDays))); n > 0 {
				h.logf("kb: removed: pruned %d post(s) older than %s", n, cfg.maxAge(textDays))
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
	cutoff := time.Now().Add(-cfg.maxAge(textDays))
	tabs, err := h.lark.MomentTabs(ctx)
	if err != nil {
		r.Error = "tabs: " + err.Error()
		return r
	}
	// Where each tab's deep pass got to (a page token), and whether it reached the end: the feed is longer than one
	// round reads, so every round reads the head for what is new and then goes on from where the last one stopped.
	deepPath := filepath.Join(st.Dir, "moments", "tabs.json")
	deep := map[string]momentsTab{}
	if raw, err := os.ReadFile(deepPath); err == nil {
		_ = json.Unmarshal(raw, &deep)
	}
	defer func() {
		raw, _ := json.MarshalIndent(deep, "", "  ")
		_ = os.WriteFile(deepPath, append(raw, '\n'), 0o600)
	}()
	categories := map[string]string{} // category id → name: a category is a tab
	for _, t := range tabs {
		if t.Type == 2 {
			categories[t.ID] = t.Name
		}
	}
	seen := map[string]bool{}
	// read pages a tab from token on; head stops at a page with nothing new. Returns the next token ("" at the end,
	// or at a page whose posts are all past max_days: nothing further down is wanted) and whether it may go on
	// (false on errors, the end, or a rate limit).
	read := func(tab lark.MomentTab, token string, pages int, head bool) (string, bool) {
		for page := 0; page < pages; page++ {
			if !wait() {
				return token, false
			}
			posts, next, err := h.lark.MomentFeed(ctx, tab.ID, token, 50)
			if err != nil {
				r.Error = tab.Name + ": " + err.Error()
				return token, false
			}
			r.Pages++
			fresh, old := 0, 0
			for _, p := range posts {
				if p.ID == "" {
					continue
				}
				if p.At.Before(cutoff) {
					old++
					continue
				}
				if seen[p.ID] { // in another tab already this round: its first sighting stands
					continue
				}
				seen[p.ID] = true
				r.Seen++
				if len(p.Categories) == 0 {
					for _, id := range p.CategoryIDs {
						if n := categories[id]; n != "" {
							p.Categories = append(p.Categories, n)
						}
					}
				}
				switch h.archiveMomentMeta(st, p, me.UserID) {
				case "new":
					r.New++
					fresh++
				case "changed":
					r.Changed++
					fresh++
				}
			}
			r.Old += old
			if next == "" || next == token || len(posts) == 0 {
				return "", false
			}
			// The feed is by heat, not time, but a whole page past max_days is as far down as a tab is worth reading.
			if old == len(posts) {
				r.TabsEnded++
				return "", false
			}
			token = next
			// Heat order: a head page with nothing new or changed means the rest is what the deep pass covers.
			if head && fresh == 0 {
				return token, true
			}
		}
		return token, true
	}
	for _, tab := range tabs {
		if tab.ID == "" {
			continue
		}
		r.Tabs++
		headToken, ok := read(tab, "", or(cfg.HeadPages, 3), true)
		if r.Error != "" && rateLimited(errors.New(r.Error)) {
			return r // back off until the next round
		}
		d := deep[tab.ID]
		if d.Done || !ok {
			if !ok && headToken == "" && r.Error == "" {
				d.Done = true // the head alone reached the end
			}
			deep[tab.ID] = d
			continue
		}
		from := d.Token
		if from == "" {
			from = headToken
		}
		next, _ := read(tab, from, or(cfg.PagesPerTab, 10), false)
		if r.Error != "" && rateLimited(errors.New(r.Error)) {
			return r
		}
		if strings.Contains(r.Error, tab.Name+":") {
			d.Token = "" // a token the feed no longer takes: start the deep pass over from the top
		} else {
			d.Token, d.Done = next, next == ""
		}
		deep[tab.ID] = d
	}
	// Comments of the posts whose count moved (newest first), within the round's budget.
	var todo []*kb.MomentPost
	for _, p := range st.MomentPosts() {
		switch {
		case p.At.Before(cutoff): // past max_days: pruned soon, its comments not worth a request
		case p.CommentsOf == p.CommentCount:
		case p.CommentCount == 0: // nothing to read: settled at once, no request, no budget
			p.CommentsOf, p.CommentsAt = 0, time.Now().UTC()
			_ = st.PutMomentPost(p, []kb.MomentComment{})
		default:
			todo = append(todo, p)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].At.After(todo[j].At) })
	fetched := 0
	for i, p := range todo {
		if fetched >= or(cfg.CommentsPerRound, 150) || ctx.Err() != nil {
			r.CommentsLeft = len(todo) - i
			break
		}
		if !wait() {
			r.CommentsLeft = len(todo) - i
			break
		}
		fetched++
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
		CommentsOf: -1, Mine: myID != "" && p.UserID == myID, Feed: "removed"}
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
