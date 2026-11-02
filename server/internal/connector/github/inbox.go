package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// The inbox (docs/plan-connectors.md「四 · 收件箱」): GitHub's unread notifications that ask something of the user —
// a review request, an @, a new comment or review on their PR / issue, their CI failing, an assignment — one thing
// per PR / issue. Polled (the hub has no public address for webhooks) at the pace GitHub asks for.

type inboxState struct {
	mu           sync.Mutex
	things       map[string]*connector.InboxThing // by thread
	seen         map[string]string                // notification id -> the updated_at already read
	lastModified string                           // of the last notifications answer, for If-Modified-Since
	fullAt       time.Time                        // the last read without If-Modified-Since
}

var (
	pollDefault  = 60 * time.Second   // when GitHub does not say (X-Poll-Interval)
	fullEvery    = 10 * time.Minute   // read everything now and then: a notification read on GitHub may not change Last-Modified
	maxDetails   = 30                 // threads read per poll; the rest wait for the next
	forgetAfter  = 8 * 24 * time.Hour // things kept for the hub to settle its items, like its own inboxForget
	contextMoves = 8                  // newest comments / reviews in a thing's context
)

type notification struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
	Subject   struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"` // PullRequest | Issue | CheckSuite | Release | Discussion…
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

// Watch polls the notifications until ctx ends, saying ScopeInbox when a thing changed.
func (c *Connector) Watch(ctx context.Context, changed func(scope string)) error {
	for {
		wait := pollDefault
		if c.signedIn() {
			w, moved, err := c.poll(ctx)
			if err != nil {
				return err
			}
			if moved {
				changed(connector.ScopeInbox)
			}
			wait = max(w, pollDefault)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// poll reads the unread notifications (not at all when nothing changed: 304) and returns how long GitHub wants
// the next poll to wait and whether a thing changed.
func (c *Connector) poll(ctx context.Context) (time.Duration, bool, error) {
	st := &c.inbox
	st.mu.Lock()
	if st.things == nil {
		st.things, st.seen = map[string]*connector.InboxThing{}, map[string]string{}
	}
	hdr := http.Header{}
	full := time.Since(st.fullAt) >= fullEvery
	if !full && st.lastModified != "" {
		hdr.Set("If-Modified-Since", st.lastModified)
	}
	st.mu.Unlock()

	var ns []notification
	code, h, err := c.request(ctx, http.MethodGet, "/notifications?per_page=50", hdr, nil, &ns)
	if err != nil {
		return 0, false, err
	}
	wait := pollDefault
	if s, err := strconv.Atoi(h.Get("X-Poll-Interval")); err == nil && s > 0 {
		wait = time.Duration(s) * time.Second
	}
	if code == http.StatusNotModified {
		return wait, false, nil
	}
	st.mu.Lock()
	st.lastModified = h.Get("Last-Modified")
	if full {
		st.fullAt = time.Now()
	}
	st.mu.Unlock()
	return wait, c.applyNotifications(ctx, ns, time.Now()), nil
}

// applyNotifications reads the threads that moved (at most maxDetails, none while requests run low) and marks
// those no longer unread as read elsewhere.
func (c *Connector) applyNotifications(ctx context.Context, ns []notification, now time.Time) bool {
	st := &c.inbox
	me := c.Status(ctx).Account
	budget := maxDetails
	if c.ratesLow() {
		budget = 0
	}
	unread := map[string]bool{}
	moved := false
	for _, n := range ns {
		kind, label, tier := classify(n)
		if kind == "" {
			continue
		}
		thread := threadOf(n)
		unread[thread] = true
		stamp := n.UpdatedAt.UTC().Format(time.RFC3339)
		st.mu.Lock()
		known := st.seen[n.ID] == stamp
		st.mu.Unlock()
		if known || budget == 0 {
			continue
		}
		budget--
		thing, err := c.describe(ctx, n, me)
		if err != nil {
			continue // read again next time
		}
		thing.Thread, thing.Kind, thing.Label, thing.Tier, thing.Last = thread, kind, label, tier, stamp
		st.mu.Lock()
		st.seen[n.ID] = stamp
		st.things[thread] = &thing
		st.mu.Unlock()
		moved = true
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for thread, t := range st.things {
		switch {
		case now.Sub(t.At) > forgetAfter:
			delete(st.things, thread)
		case !unread[thread] && !t.Read && t.Done == "":
			t.Read, moved = true, true
		}
	}
	return moved
}

// classify says whether a notification is for the inbox: its kind, its tag and the tier without a model.
func classify(n notification) (kind, label, tier string) {
	const action, fyi = "action", "fyi"
	switch n.Reason {
	case "review_requested":
		return "review_requested", "请你 review", action
	case "mention":
		return "mention", "@我", action
	case "team_mention":
		return "team_mention", "@了你的团队", fyi
	case "assign":
		return "assign", "分配给你", action
	case "author":
		if n.Subject.Type == "PullRequest" {
			return "author", "你的 PR 有新动静", fyi
		}
		if n.Subject.Type == "Issue" {
			return "author", "你的 issue 有新动静", fyi
		}
	case "ci_activity":
		if strings.Contains(strings.ToLower(n.Subject.Title), "fail") {
			return "ci_failed", "CI 失败", action
		}
	}
	return "", "", ""
}

var subjectURL = regexp.MustCompile(`/repos/([^/]+/[^/]+)/(?:issues|pulls)/(\d+)$`)

// threadOf is the PR / issue a notification is about ("example-org/example-repo#12"), so a review request and an @
// on the same PR are one thing; anything else is its own notification.
func threadOf(n notification) string {
	if m := subjectURL.FindStringSubmatch(n.Subject.URL); m != nil {
		return m[1] + "#" + m[2]
	}
	return "notification:" + n.ID
}

// move is a comment or a review, for the preview and the context.
type move struct {
	who, verb, text string
	at              time.Time
}

// describe reads what the notification is about: the PR / issue, its newest comments and reviews, whether it is
// over (merged, closed, the review request withdrawn) or the user has answered.
func (c *Connector) describe(ctx context.Context, n notification, me string) (connector.InboxThing, error) {
	t := connector.InboxThing{Title: n.Subject.Title, At: n.UpdatedAt}
	m := subjectURL.FindStringSubmatch(n.Subject.URL)
	if m == nil { // a CI run: there is nothing to read or answer, only the failure to look at
		t.Sender, t.Preview, t.URL = "GitHub Actions", n.Subject.Title, n.Repository.HTMLURL+"/actions"
		t.Context = fmt.Sprintf("仓库 %s 的 CI（GitHub Actions）：%s", n.Repository.FullName, n.Subject.Title)
		return t, nil
	}
	repo, number := m[1], m[2]
	var it struct {
		Title     string `json:"title"`
		State     string `json:"state"`
		User      user   `json:"user"`
		Body      string `json:"body"`
		HTMLURL   string `json:"html_url"`
		Comments  int    `json:"comments"`
		Merged    bool   `json:"merged"`
		Locked    bool   `json:"locked"`
		Reviewers []user `json:"requested_reviewers"`
		Teams     []struct {
			Slug string `json:"slug"`
		} `json:"requested_teams"`
	}
	pr := n.Subject.Type == "PullRequest"
	path := fmt.Sprintf("/repos/%s/issues/%s", repo, number)
	if pr {
		path = fmt.Sprintf("/repos/%s/pulls/%s", repo, number)
	}
	if _, err := c.Get(ctx, path, &it); err != nil {
		return t, err
	}
	if pr { // a PR's conversation comments are counted on its issue
		var iss struct {
			Comments int `json:"comments"`
		}
		if _, err := c.Get(ctx, fmt.Sprintf("/repos/%s/issues/%s", repo, number), &iss); err != nil {
			return t, err
		}
		it.Comments = iss.Comments
	}
	var moves []move
	var cs []comment
	page := max(1, (it.Comments+99)/100)
	if _, err := c.Get(ctx, fmt.Sprintf("/repos/%s/issues/%s/comments?per_page=100&page=%d", repo, number, page), &cs); err != nil {
		return t, err
	}
	for _, cm := range cs {
		moves = append(moves, move{who(cm.User), "", strings.TrimSpace(cm.Body), cm.CreatedAt})
	}
	reviewed := false
	if pr {
		var rs []comment
		if _, err := c.Get(ctx, fmt.Sprintf("/repos/%s/pulls/%s/reviews?per_page=100", repo, number), &rs); err != nil {
			return t, err
		}
		for _, r := range rs {
			verb := map[string]string{"APPROVED": "批准了", "CHANGES_REQUESTED": "要求修改", "COMMENTED": "评审了"}[r.State]
			if verb == "" || r.State == "COMMENTED" && strings.TrimSpace(r.Body) == "" {
				continue
			}
			if strings.EqualFold(r.User.Login, me) {
				reviewed = true
			}
			moves = append(moves, move{who(r.User), verb, strings.TrimSpace(r.Body), r.SubmittedAt})
		}
	}
	sort.SliceStable(moves, func(i, j int) bool { return moves[i].at.Before(moves[j].at) })

	what := "issue"
	if pr {
		what = "PR"
	}
	ref := repo + "#" + number
	t.Title, t.URL = it.Title, it.HTMLURL
	t.ReplyTo, t.CanReply = "评论到 "+ref+"（仓库可见的人都能看到）", !it.Locked
	t.Sender, t.Preview = it.User.String(), firstLine(it.Body)
	if len(moves) > 0 {
		last := moves[len(moves)-1]
		t.Sender, t.Preview = last.who, strings.TrimSpace(last.verb+" "+firstLine(last.text))
	}

	switch {
	case it.Merged || it.State == "closed":
		t.Done = connector.DoneRead
	case len(moves) > 0 && strings.EqualFold(strings.TrimSuffix(moves[len(moves)-1].who, " (bot)"), me):
		t.Done = connector.DoneReplied
	case n.Reason == "review_requested" && len(it.Teams) == 0 && !hasLogin(it.Reviewers, me):
		// no longer asked: reviewed (answered) or the request withdrawn
		if reviewed {
			t.Done = connector.DoneReplied
		} else {
			t.Done = connector.DoneRead
		}
	}

	var b strings.Builder
	state := it.State
	if it.Merged {
		state = "merged"
	}
	fmt.Fprintf(&b, "GitHub %s %s：%s\n状态：%s · 作者 %s", what, ref, it.Title, state, it.User)
	if rv := logins(it.Reviewers); rv != "" {
		fmt.Fprintf(&b, " · 等这些人 review：%s", rv)
	}
	fmt.Fprintf(&b, "\n%s\n\n描述：\n%s", it.HTMLURL, cut(strings.TrimSpace(it.Body), 1500, "（没有描述）"))
	if len(moves) > 0 {
		from := max(0, len(moves)-contextMoves)
		fmt.Fprintf(&b, "\n\n最近的评论和 review（旧的在前，共 %d 条）：", len(moves))
		for _, mv := range moves[from:] {
			fmt.Fprintf(&b, "\n\n%s %s", day(mv.at), mv.who)
			if mv.verb != "" {
				b.WriteString(" " + mv.verb)
			}
			if mv.text != "" {
				b.WriteString("：\n" + cut(mv.text, 600, ""))
			}
		}
	}
	t.Context = b.String()
	return t, nil
}

func hasLogin(us []user, login string) bool {
	for _, u := range us {
		if strings.EqualFold(u.Login, login) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > 140 {
		s = string(r[:140]) + "…"
	}
	return s
}

// Collect is every thing seen lately, from memory (Watch keeps it).
func (c *Connector) Collect(context.Context) ([]connector.InboxThing, error) {
	st := &c.inbox
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]connector.InboxThing, 0, len(st.things))
	for _, t := range st.things {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

var threadRef = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#(\d+)$`)

// Reply comments text on the PR / issue (the user pressed send on it).
func (c *Connector) Reply(ctx context.Context, thread, text string) error {
	m := threadRef.FindStringSubmatch(thread)
	if m == nil || !validRepo(m[1]) {
		return fmt.Errorf("%s cannot be replied to", thread)
	}
	body, _ := json.Marshal(map[string]string{"body": text})
	if _, _, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%s/comments", m[1], m[2]), nil, body, nil); err != nil {
		return err
	}
	st := &c.inbox
	st.mu.Lock()
	if t := st.things[thread]; t != nil {
		t.Done = connector.DoneReplied
	}
	st.mu.Unlock()
	return nil
}
