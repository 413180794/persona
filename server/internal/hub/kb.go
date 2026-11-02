package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// KBConfig tunes the knowledge base under ~/.jarvis/data (docs/knowledge.md).
type KBConfig struct {
	Enabled        *bool      `json:"enabled,omitempty"`         // default on when Feishu or mail is set up
	TextDays       int        `json:"text_days,omitempty"`       // full text kept (365)
	AttachmentDays int        `json:"attachment_days,omitempty"` // images / files kept (30)
	AI             *ai.Config `json:"ai,omitempty"`              // compression model (default inbox.triage_ai, then ai)
	// ExtraDirs are directories on the hub's machine that kb_search also reads (Markdown, read only), e.g. a
	// repository of notes the user keeps; "~/" is the home there (docs/knowledge.md).
	ExtraDirs []string `json:"extra_dirs,omitempty"`
	// Moments archives the whole removed feed under <data>/moments (kb_moments_crawl.go); off unless enabled.
	Moments *MomentsCrawl `json:"moments,omitempty"`
}

const (
	kbPoll        = 2 * time.Minute
	kbDebounce    = 5 * time.Second
	kbMailPoll    = 6 * time.Minute
	kbItemsPoll   = 30 * time.Minute
	kbCompressMax = 8 // chats compressed per run
)

type kbState struct {
	once sync.Once
	kick chan struct{}
	eng  *kb.Engine
	cfg  KBConfig
}

func (h *Hub) kbKick() {
	if h.kb.kick == nil {
		return
	}
	select {
	case h.kb.kick <- struct{}{}:
	default:
	}
}

// kbIndex is the optional Postgres index (internal/db implements it).
type kbIndex interface {
	PutKBEntity(ctx context.Context, kind, id, name, importance string, relevance int, tags []string, oneLine string, updated time.Time) error
}

func (h *Hub) startKB(ctx context.Context, cfg KBConfig, dir string) {
	if cfg.Enabled != nil && !*cfg.Enabled {
		return
	}
	st, err := kb.NewStore(filepath.Join(dir, "data"))
	if err != nil {
		h.logf("kb disabled: %v", err)
		return
	}
	h.rezoneKB(st)
	model := h.triageModel()
	if cfg.AI != nil {
		model = ai.New(cfg.AI.Over(h.cfg.AI))
	}
	e := &kb.Engine{Store: st, Model: model, ModelFor: func(id string) kb.Model { return h.purposeModel(id) }, Owner: h.ownerName(), Logf: h.logf,
		TextDays: cfg.TextDays, AttachmentDays: cfg.AttachmentDays}
	for _, d := range cfg.ExtraDirs {
		if d = expandHome(strings.TrimSpace(d)); filepath.IsAbs(d) {
			e.ExtraDirs = append(e.ExtraDirs, d)
		}
	}
	e.Sources = h.kbSources()
	if idx, ok := h.store.(kbIndex); ok {
		e.Index = func(kind, id, name string, t kb.Tagging, one string, updated time.Time) {
			ictx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := idx.PutKBEntity(ictx, kind, id, name, t.Importance, t.Relevance, t.Tags, one, updated); err != nil {
				h.logf("kb index: %v", err)
			}
		}
	}
	h.kb.eng, h.kb.cfg, h.kb.kick = e, cfg, make(chan struct{}, 1)
	// Projects from before keywords existed get them at their next rewrite: the active ones are rewritten now (a
	// few a run), so their removed posts can be found.
	if n := e.SeedKeywords(60 * 24 * time.Hour); n > 0 {
		h.logf("kb: %d project(s) to be rewritten for their keywords", n)
	}
	if n := e.IgnoreNoiseProjects(); n > 0 {
		h.logf("kb: %d project(s) from notifications marked ignored", n)
	}
	if n := e.RetagMail(); n > 0 {
		h.logf("kb: %d mail chat(s) queued to be tagged again under the current rules", n)
	}
	go h.kbLoop(ctx)
	if m := cfg.Moments; m != nil && m.Enabled && h.lark != nil {
		days := cfg.TextDays
		if days <= 0 {
			days = 365
		}
		go h.momentsCrawlLoop(ctx, st, *m, days)
	}
}

// kbSources are where the knowledge base archives chats from, chat by chat: every connector's (Feishu's chats and
// removed posts), in the order they were registered.
func (h *Hub) kbSources() []kb.Source {
	var out []kb.Source
	for _, c := range h.connectors.All() {
		if cs, ok := c.(connector.Chats); ok {
			out = append(out, cs.ChatSources()...)
		}
	}
	return out
}

// kbCollectItems records every connector's items (Feishu's documents, meetings and events) and tags new ones.
func (h *Hub) kbCollectItems(ctx context.Context) {
	e := h.kb.eng
	any := false
	for _, c := range h.connectors.All() {
		if is, ok := c.(connector.Items); ok {
			any = true
			if err := e.CollectItems(ctx, is.ItemSource()); err != nil {
				h.logf("kb: items: %v", err)
			}
		}
	}
	if !any {
		return
	}
	if err := e.TagItems(ctx); err != nil && !ai.IsPaused(err) {
		h.logf("kb: tag items: %v", err)
	}
}

// rezoneKB keeps the archive in the user's time zone (kb/zone.go): the days and times written before (the hub's own
// zone, UTC) or before the user moved are moved into it.
func (h *Hub) rezoneKB(st *kb.Store) {
	loc := h.userLocation()
	n, err := st.Rezone(loc)
	if n > 0 {
		h.logf("kb: %d chat(s) / timeline(s) moved into %s", n, loc)
	}
	if err != nil {
		h.logf("kb: %v", err)
	}
}

func (h *Hub) kbLoop(ctx context.Context) {
	e := h.kb.eng
	var lastMail, lastItems, lastPrune, lastLinks, lastConnectors time.Time
	t := time.NewTicker(kbPoll)
	defer t.Stop()
	for {
		run(ctx, func() {
			h.rezoneKB(e.Store)
			if _, err := e.Archive(ctx); err != nil {
				h.logf("kb: archive: %v", err)
			}
		})
		if h.mail != nil && time.Since(lastMail) >= kbMailPoll {
			lastMail = time.Now()
			h.kbMail(ctx)
		}
		if time.Since(lastConnectors) >= kbConnectorPoll {
			lastConnectors = time.Now()
			run(ctx, func() { h.kbConnectors(ctx) })
		}
		for i, c := range e.DueForCompression(time.Now()) {
			if i >= kbCompressMax || ctx.Err() != nil {
				break
			}
			if err := e.Compress(ctx, c); err != nil {
				if ai.IsPaused(err) { // the chats stay due: compressed once the connection is back
					break
				}
				h.logf("kb: compress %s: %v", c.ID, err)
			}
		}
		e.RefreshProjects(ctx, 3)
		// Same work under two names, and the removed posts about each project: a few model calls every half hour.
		if time.Since(lastLinks) >= kbItemsPoll {
			lastLinks = time.Now()
			run(ctx, func() {
				if n := e.DedupeProjects(ctx, 4); n > 0 {
					h.logf("kb: merged %d duplicate project(s)", n)
				}
				if n := e.LinkMoments(ctx, 3); n > 0 {
					h.logf("kb: linked %d removed post(s) to projects", n)
				}
			})
		}
		run(ctx, func() {
			if n, err := e.Backfill(ctx, 5, 3); err != nil {
				h.logf("kb: backfill: %v", err)
			} else if n > 0 {
				h.logf("kb: backfilled %d line(s)", n)
			}
		})
		if time.Since(lastItems) >= kbItemsPoll {
			lastItems = time.Now()
			h.kbCollectItems(ctx)
		}
		if time.Since(lastPrune) >= 24*time.Hour {
			lastPrune = time.Now()
			if n := e.Prune(time.Now()); n > 0 {
				h.logf("kb: pruned %d day(s)", n)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-h.kb.kick:
			select {
			case <-ctx.Done():
				return
			case <-time.After(kbDebounce):
			}
		}
	}
}

// run calls f, turning a panic into a log line: one malformed message must not stop the loop.
func run(ctx context.Context, f func()) {
	defer func() { _ = recover() }()
	if ctx.Err() == nil {
		f()
	}
}

// --- Feishu ---

type larkSource struct{ c *lark.Client }

func (s *larkSource) Name() string { return "lark" }

func (s *larkSource) Chats(ctx context.Context, limit int) ([]kb.SourceChat, error) {
	var out []kb.SourceChat
	cursor := ""
	for len(out) < limit {
		page, next, err := s.c.Conversations(ctx, "chats", cursor, min(100, limit-len(out)))
		if err != nil {
			return out, err
		}
		for _, c := range page {
			kind := "group"
			if !c.IsGroup {
				kind = "dm"
				if larkKind(c) == KindBot {
					kind = "bot"
				}
			}
			out = append(out, kb.SourceChat{ID: c.ID, Name: c.Name, Kind: kind, Topic: c.IsTopic, UpdatedAt: c.UpdatedAt})
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return out, nil
}

var larkMediaRe = regexp.MustCompile(`"(/api/v1/lark/(?:media|messages)/[^"]+)"`)

func (s *larkSource) Messages(ctx context.Context, c kb.SourceChat, after string, limit int) ([]kb.SourceMessage, string, bool, error) {
	if !larkPosition(after) {
		after = "" // a cursor lark-connector rejects (one came back as "-3"): read the newest page; older lines are skipped by time
	}
	w := lark.Window{After: after}
	if c.Topic {
		w = lark.Window{}
	}
	p, err := s.c.Messages(ctx, c.ID, w, limit, c.Topic)
	if err != nil {
		return nil, "", false, err
	}
	out := make([]kb.SourceMessage, 0, len(p.Messages))
	for _, m := range p.Messages {
		if m.Recalled {
			continue
		}
		sm := kb.SourceMessage{ID: m.ID, Position: m.Position, SenderID: m.SenderID, SenderName: m.SenderName, Mine: m.IsMine,
			Type: m.Type, Text: m.Text, At: m.CreatedAt}
		if m.ReplyTo != nil && m.ReplyTo.Text != "" {
			sm.Text = fmt.Sprintf("（回复 %s：%s）%s", m.ReplyTo.SenderName, clipRunes(m.ReplyTo.Text, 60), sm.Text)
		}
		if m.Type == "IMAGE" || m.Type == "FILE" || m.Type == "POST" {
			for _, mm := range larkMediaRe.FindAllStringSubmatch(string(m.Parts), 4) {
				sm.Media = append(sm.Media, mm[1])
			}
		}
		out = append(out, sm)
	}
	newer := p.NewerCursor
	if c.Topic || !larkPosition(newer) {
		newer = ""
	}
	return out, newer, p.HasNewer, nil
}

// larkPosition: a message position lark-connector takes back as a cursor, a non-negative integer (or none). Once it handed
// out "-3" for a chat; kept as the archive cursor, every later read of that chat failed with INVALID_CURSOR.
func larkPosition(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.ParseInt(p, 10, 64)
	return err == nil && n >= 0 && n <= 1<<53-1
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

const kbMaxAttachment = 20 << 20

func (s *larkSource) Download(ctx context.Context, media, dst string) error {
	u, err := url.Parse(media)
	if err != nil {
		return err
	}
	path, ok := strings.CutPrefix(u.Path, "/api/v1/lark")
	if !ok {
		return errors.New("not a Feishu media path")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dst+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := &fileResponse{f: f, h: http.Header{}}
	err = s.c.Media(ctx, w, path, u.Query(), http.Header{})
	f.Close()
	if err == nil && w.over {
		err = errors.New("attachment over 20 MB")
	}
	if err != nil {
		os.Remove(dst + ".part")
		return err
	}
	return os.Rename(dst+".part", dst)
}

// fileResponse is an http.ResponseWriter that writes the body to a file, up to kbMaxAttachment.
type fileResponse struct {
	f    *os.File
	h    http.Header
	n    int64
	over bool
}

func (w *fileResponse) Header() http.Header { return w.h }
func (w *fileResponse) WriteHeader(int)     {}
func (w *fileResponse) Write(p []byte) (int, error) {
	if w.n+int64(len(p)) > kbMaxAttachment {
		w.over = true
		return 0, errors.New("too large")
	}
	w.n += int64(len(p))
	return w.f.Write(p)
}

// calendarWindow is what the knowledge base reads of the calendar: from the user's yesterday to a week after their
// today, midnight to midnight where they are, as UTC instants (it was the hub's UTC date with a Z: up to a day off).
func calendarWindow(now time.Time, loc *time.Location) (start, end string) {
	y, m, d := now.In(loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	return today.AddDate(0, 0, -1).UTC().Format(time.RFC3339), today.AddDate(0, 0, 8).UTC().Format(time.RFC3339)
}

// loc is the user's time zone (calendarWindow).
type larkItems struct {
	c   *lark.Client
	loc *time.Location
}

func (s *larkItems) Items(ctx context.Context) ([]kb.Item, error) {
	var out []kb.Item
	start, end := calendarWindow(time.Now(), s.loc)
	q := url.Values{"start": {start}, "end": {end}}
	if raw, err := s.c.Read(ctx, "GET", "/calendar/week", q, nil); err == nil {
		var cal struct {
			Events []struct {
				ID, SourceID, Title string
				StartAt             time.Time
				IsFree              bool
			}
		}
		if json.Unmarshal(raw, &cal) == nil {
			for _, ev := range cal.Events {
				if ev.Title == "" || ev.IsFree {
					continue
				}
				id := ev.SourceID
				if id == "" {
					id = ev.ID
				}
				out = append(out, kb.Item{Kind: kb.KindEvent, ID: id, Title: ev.Title, At: ev.StartAt, Where: "calendar"})
			}
		}
	} else {
		return nil, err
	}
	if raw, err := s.c.Read(ctx, "GET", "/meetings", url.Values{"count": {"30"}}, nil); err == nil {
		var ms struct {
			Items []struct {
				HistoryID string    `json:"historyId"`
				Topic     string    `json:"topic"`
				SortedAt  time.Time `json:"sortedAt"`
				Kind      string    `json:"kind"`
			}
		}
		if json.Unmarshal(raw, &ms) == nil {
			for _, m := range ms.Items {
				if m.Topic == "" || m.Kind != "" && m.Kind != "meeting" {
					continue
				}
				out = append(out, kb.Item{Kind: kb.KindMeeting, ID: m.HistoryID, Title: m.Topic, At: m.SortedAt, Where: "meetings"})
			}
		}
	}
	return out, nil
}

// DocTitle reads the Markdown export's first line ("# 标题").
func (s *larkItems) DocTitle(ctx context.Context, u string) string {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := s.c.Read(cctx, "GET", "/documents/read", url.Values{"url": {u}, "format": {"markdown"}}, nil)
	if err != nil {
		return ""
	}
	var doc struct {
		Markdown string `json:"markdown"`
		Content  string `json:"content"`
		Title    string `json:"title"`
	}
	text := ""
	if json.Unmarshal(raw, &doc) == nil {
		if doc.Title != "" {
			return doc.Title
		}
		text = doc.Markdown + doc.Content
	} else {
		text = string(raw)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return clipRunes(strings.TrimSpace(strings.TrimPrefix(first, "#")), 120)
}

// --- mail ---

// mailSource lets kb.Ingest save mail: one chat per account and correspondent.
type mailSource struct{}

func (mailSource) Name() string                                        { return "mail" }
func (mailSource) Chats(context.Context, int) ([]kb.SourceChat, error) { return nil, nil }
func (mailSource) Download(context.Context, string, string) error {
	return errors.New("no mail attachments")
}
func (mailSource) Messages(context.Context, kb.SourceChat, string, int) ([]kb.SourceMessage, string, bool, error) {
	return nil, "", false, nil
}

// kbMail archives inbox mail newer than the last UID seen per account (subject + text, 4 000 characters).
func (h *Hub) kbMail(ctx context.Context) {
	e := h.kb.eng
	cursorsFile := filepath.Join(e.Store.Dir, "mail-cursors.json")
	cursors := map[string]uint32{}
	if raw, err := os.ReadFile(cursorsFile); err == nil {
		_ = json.Unmarshal(raw, &cursors)
	}
	accounts, err := h.mail.ListAccounts()
	if err != nil {
		return
	}
	reads := 0
	for _, a := range accounts {
		res, err := h.mail.ListMessages(ctx, mail.ListOptions{AccountID: a.ID, Limit: 40})
		if err != nil {
			continue
		}
		last := cursors[a.ID]
		top := last
		for i := len(res.Messages) - 1; i >= 0; i-- { // oldest first
			m := res.Messages[i]
			if m.UID <= last {
				continue
			}
			if m.UID > top {
				top = m.UID
			}
			text := m.Snippet
			if reads < 10 {
				if full, err := h.mail.GetMessage(ctx, a.ID, m.UID, m.Mailbox); err == nil && strings.TrimSpace(full.Text) != "" {
					text = full.Text
				}
				reads++
			}
			at, _ := time.Parse(time.RFC3339, m.Date)
			addr := strings.ToLower(m.From.Email)
			name := m.From.Name
			if name == "" {
				name = addr
			}
			sc := kb.SourceChat{ID: a.ID + "-" + addr, Name: name + " · 邮件", Kind: "mail"}
			sm := kb.SourceMessage{ID: fmt.Sprint(m.UID), SenderID: addr, SenderName: name, At: at, Type: "TEXT",
				Mine: strings.EqualFold(addr, a.Email),
				Text: "【" + m.Subject + "】" + clipRunes(strings.TrimSpace(text), 4000)}
			if _, err := e.Ingest(ctx, mailSource{}, sc, []kb.SourceMessage{sm}); err != nil {
				h.logf("kb: mail %s: %v", a.Email, err)
			}
		}
		cursors[a.ID] = top
	}
	if raw, err := json.Marshal(cursors); err == nil {
		_ = os.WriteFile(cursorsFile, raw, 0o600)
	}
}

// --- assistant tools (definitions in prompts/mcp/kb) ---

func (h *Hub) runKBTool(name string, args json.RawMessage) (string, error) {
	e := h.kb.eng
	if e == nil {
		return "", errors.New("the knowledge base is off (hub.json kb.enabled)")
	}
	switch name {
	case "kb_search":
		a := struct {
			Query    string `json:"query"`
			Messages *bool  `json:"messages"`
			Limit    int    `json:"limit"`
		}{}
		_ = json.Unmarshal(args, &a)
		if a.Limit <= 0 {
			a.Limit = 40
		}
		hits := e.Search(a.Query, a.Messages == nil || *a.Messages, min(a.Limit, 200))
		if len(hits) == 0 {
			return "No match in " + e.Store.Dir + ".", nil
		}
		var b strings.Builder
		for _, hit := range hits {
			fmt.Fprintf(&b, "%s: %s\n", hit.Path, hit.Line)
		}
		return b.String(), nil
	case "kb_list":
		var a struct {
			Kind, Tag string
			Limit     int
		}
		_ = json.Unmarshal(args, &a)
		list := e.List(a.Kind, a.Tag)
		if a.Limit <= 0 {
			a.Limit = 30
		}
		if len(list) > a.Limit {
			list = list[:a.Limit]
		}
		if len(list) == 0 {
			return "Nothing yet.", nil
		}
		var b strings.Builder
		for _, x := range list {
			fmt.Fprintf(&b, "- %s · %s [%s 相关%d] %s %s%s\n", x.ID, x.Name, orDash(x.Importance), x.Relevance,
				strings.Join(x.Tags, ","), x.Extra, prefix(" — ", x.OneLine))
		}
		return b.String(), nil
	case "kb_get":
		var a struct{ Kind, ID string }
		_ = json.Unmarshal(args, &a)
		if a.Kind == kb.KindProject {
			if p := e.FindProject(a.ID); p != nil {
				a.ID = p.Slug
			}
		}
		d, err := e.Get(a.Kind, a.ID, 12000)
		if err != nil {
			return "", fmt.Errorf("%s/%s: %w", a.Kind, a.ID, err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Path: %s\n\nmeta.json:\n%s\n", d.Path, d.Meta)
		if d.Summary != "" {
			fmt.Fprintf(&b, "\nsummary.md:\n%s\n", d.Summary)
		}
		if len(d.Log) > 0 {
			fmt.Fprintf(&b, "\nlog.md (latest):\n%s\n", strings.Join(d.Log, "\n"))
		}
		if len(d.Moments) > 0 {
			b.WriteString("\nremoved posts about it (full text: moments/posts/<id>.md):\n")
			for _, m := range d.Moments {
				fmt.Fprintf(&b, "- %s %s · %d comments · %s — %s\n", m.ID, m.At.In(h.userLocation()).Format("2006-01-02"), m.Comments, m.Note, m.Text)
			}
		}
		if d.Recent != "" {
			fmt.Fprintf(&b, "\nMessages, last 7 days:\n%s\n", d.Recent)
		}
		return b.String(), nil
	case "kb_update":
		var a struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
			kb.Update
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		id, err := e.Apply(a.Kind, a.ID, a.Update)
		if err != nil {
			return "", err
		}
		return "Saved " + a.Kind + "/" + id + ".", nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func prefix(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

// kbContextFor is what the knowledge base knows about an inbox item's chat and its projects, for the inbox agent
// (AI 分身): the compressed context makes the draft sound like the owner, and the agent writes conclusions back.
func (h *Hub) kbContextFor(it *InboxItem) *prompts.KBContext {
	e := h.kb.eng
	if e == nil {
		return nil
	}
	var c *kb.Chat
	switch {
	case it.Lark != nil:
		c, _ = e.Store.Chat(kb.ChatID("lark", it.Lark.FeedID))
	case it.Mail != nil:
		c, _ = e.Store.Chat(kb.ChatID("mail", it.Mail.AccountID+"-"+strings.ToLower(it.Mail.From.Email)))
	}
	if c == nil {
		return nil
	}
	return h.kbContext(e, c)
}

// kbContext is a chat's summary and its first three projects' (shared/kb_blocks.md).
func (h *Hub) kbContext(e *kb.Engine, c *kb.Chat) *prompts.KBContext {
	out := &prompts.KBContext{ChatID: c.ID, ChatName: c.Name}
	if sum := e.Store.Summary(kb.KindChat, c.ID); sum != "" {
		out.Summary = clipRunes(sum, 4000)
	}
	for i, slug := range c.Projects {
		if i >= 3 {
			break
		}
		if p, ok := e.Store.Project(slug); ok {
			out.Projects = append(out.Projects, prompts.KBProject{Name: p.Name, Summary: clipRunes(e.Store.Summary(kb.KindProject, slug), 3000)})
		}
	}
	return out
}

// --- app API (docs/protocol.md 知识库) ---

func (h *Hub) kbRoutes(api *http.ServeMux) {
	kinds := map[string]bool{kb.KindProject: true, kb.KindChat: true, kb.KindPerson: true, kb.KindDoc: true, kb.KindMeeting: true, kb.KindEvent: true}
	engine := func(w http.ResponseWriter) *kb.Engine {
		if h.kb.eng == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("the knowledge base is off"))
		}
		return h.kb.eng
	}
	api.HandleFunc("GET /api/v1/kb/{kind}", func(w http.ResponseWriter, r *http.Request) {
		e := engine(w)
		kind := r.PathValue("kind")
		if e == nil {
			return
		}
		if !kinds[kind] {
			writeErr(w, http.StatusNotFound, errors.New("unknown kind"))
			return
		}
		// The tags offered are this kind's (all of them, not only the filtered list's): a tag from another kind
		// filtered to nothing.
		all := e.List(kind, "")
		list := all
		if tag := r.URL.Query().Get("tag"); tag != "" {
			list = e.List(kind, tag)
		}
		if list == nil {
			list = []kb.Entry{}
		}
		writeJSON(w, map[string]any{"items": list, "tags": kb.KindTags(all, 30)})
	})
	// One archived removed post (a project's 相关removed): what the crawler kept, text and comments as Markdown.
	api.HandleFunc("GET /api/v1/kb/moments/{id}", func(w http.ResponseWriter, r *http.Request) {
		e := engine(w)
		if e == nil {
			return
		}
		p, md, ok := e.Store.MomentArchived(r.PathValue("id"))
		if !ok {
			writeErr(w, http.StatusNotFound, kb.ErrNotFound)
			return
		}
		writeJSON(w, map[string]any{"post": p, "markdown": md})
	})
	api.HandleFunc("GET /api/v1/kb/{kind}/{id}", func(w http.ResponseWriter, r *http.Request) {
		e := engine(w)
		if e == nil {
			return
		}
		d, err := e.Get(r.PathValue("kind"), r.PathValue("id"), 20000)
		if errors.Is(err, kb.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		} else if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		out := map[string]any{"detail": d}
		// Opened from a search: every place the query is in it, so the page can start there.
		if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
			m := e.Matches(d.Kind, d.ID, q, 40)
			if m == nil {
				m = []kb.Match{}
			}
			out["matches"] = m
		}
		// A project's Agents: the live sessions working in its code directories.
		if d.Kind == kb.KindProject {
			if p, ok := e.Store.Project(d.ID); ok && len(p.Dirs) > 0 {
				var agents []any
				for _, m := range h.machines {
					for _, s := range h.decorated(m) {
						if s.Assistant || s.Status == proto.StatusOffline {
							continue
						}
						for _, pp := range e.ProjectsForDir(m.cfg.ID, s.Cwd) {
							if pp.Slug == p.Slug {
								agents = append(agents, s)
								break
							}
						}
					}
				}
				out["agents"] = agents
			}
		}
		writeJSON(w, out)
	})
	// One day of a chat's archive, for reading a found message in place.
	api.HandleFunc("GET /api/v1/kb/chats/{id}/days/{day}", func(w http.ResponseWriter, r *http.Request) {
		e := engine(w)
		if e == nil {
			return
		}
		msgs, prev, next, err := e.Day(r.PathValue("id"), r.PathValue("day"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, map[string]any{"day": r.PathValue("day"), "messages": msgs, "prev": prev, "next": next})
	})
	api.HandleFunc("PUT /api/v1/kb/{kind}/{id}", func(w http.ResponseWriter, r *http.Request) {
		e := engine(w)
		if e == nil {
			return
		}
		var u kb.Update
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&u); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		id, err := e.Apply(r.PathValue("kind"), r.PathValue("id"), u)
		if errors.Is(err, kb.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		} else if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"id": id})
	})
	api.HandleFunc("GET /api/v1/kb-search", func(w http.ResponseWriter, r *http.Request) {
		e := engine(w)
		if e == nil {
			return
		}
		// results=1: entries ranked, a few lines each (the app since 2026-10-31); otherwise the lines as found.
		if r.URL.Query().Get("results") == "1" {
			res := e.Find(r.URL.Query().Get("q"), 80)
			if res == nil {
				res = []kb.Result{}
			}
			writeJSON(w, map[string]any{"results": res})
			return
		}
		hits := e.Search(r.URL.Query().Get("q"), r.URL.Query().Get("messages") != "0", 60)
		if hits == nil {
			hits = []kb.Hit{}
		}
		writeJSON(w, map[string]any{"hits": hits})
	})
}

// kbAgentDone files an Agent's finished turn under the projects whose code directories it works in: one timeline
// line with what it said last, at most one per agent and 30 minutes (a busy agent finishes many short turns).
func (h *Hub) kbAgentDone(machine string, s proto.Session) {
	e := h.kb.eng
	if e == nil || strings.TrimSpace(s.LastText) == "" {
		return
	}
	projects := e.ProjectsForDir(machine, s.Cwd)
	if len(projects) == 0 {
		return
	}
	key := machine + "/" + s.SessionID
	h.mu.Lock()
	if h.kbAgentLogged == nil {
		h.kbAgentLogged = map[string]time.Time{}
	}
	if time.Since(h.kbAgentLogged[key]) < 30*time.Minute {
		h.mu.Unlock()
		return
	}
	h.kbAgentLogged[key] = time.Now()
	h.mu.Unlock()
	title := s.Title
	if title == "" || title == "Claude Code" {
		title = filepath.Base(s.Cwd)
	}
	line := clipRunes(strings.Join(strings.Fields(s.LastText), " "), 160)
	for _, p := range projects {
		if _, err := e.Apply(kb.KindProject, p.Slug, kb.Update{Log: line, Source: "Agent · " + title + " · " + machine}); err != nil {
			h.logf("kb: agent log %s: %v", p.Slug, err)
		}
	}
}

func (s *larkSource) Older(ctx context.Context, c kb.SourceChat, before string, limit int) ([]kb.SourceMessage, string, bool, error) {
	p, err := s.c.Messages(ctx, c.ID, lark.Window{Before: before}, limit, c.Topic)
	if err != nil {
		return nil, "", false, err
	}
	out := make([]kb.SourceMessage, 0, len(p.Messages))
	for _, m := range p.Messages {
		if m.Recalled {
			continue
		}
		out = append(out, kb.SourceMessage{ID: m.ID, Position: m.Position, SenderID: m.SenderID, SenderName: m.SenderName,
			Mine: m.IsMine, Type: m.Type, Text: m.Text, At: m.CreatedAt})
	}
	return out, p.OlderCursor, p.HasOlder, nil
}
