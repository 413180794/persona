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
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// KBConfig tunes the knowledge base under ~/.jarvis/data (docs/knowledge.md).
type KBConfig struct {
	Enabled        *bool      `json:"enabled,omitempty"`         // default on when Feishu or mail is set up
	TextDays       int        `json:"text_days,omitempty"`       // full text kept (365)
	AttachmentDays int        `json:"attachment_days,omitempty"` // images / files kept (30)
	AI             *ai.Config `json:"ai,omitempty"`              // compression model (default inbox.triage_ai, then ai)
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
	model := h.triageModel()
	if cfg.AI != nil {
		model = ai.New(cfg.AI.Over(h.cfg.AI))
	}
	e := &kb.Engine{Store: st, Model: model, Owner: h.ownerName(), Logf: h.logf}
	if h.lark != nil {
		e.Sources = append(e.Sources, &larkSource{c: h.lark})
	}
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
	go h.kbLoop(ctx)
}

func (h *Hub) kbLoop(ctx context.Context) {
	e := h.kb.eng
	textDays, attDays := h.kb.cfg.TextDays, h.kb.cfg.AttachmentDays
	if textDays <= 0 {
		textDays = 365
	}
	if attDays <= 0 {
		attDays = 30
	}
	var lastMail, lastItems, lastPrune time.Time
	t := time.NewTicker(kbPoll)
	defer t.Stop()
	for {
		run(ctx, func() {
			if _, err := e.Archive(ctx); err != nil {
				h.logf("kb: archive: %v", err)
			}
		})
		if h.mail != nil && time.Since(lastMail) >= kbMailPoll {
			lastMail = time.Now()
			h.kbMail(ctx)
		}
		for i, c := range e.DueForCompression(time.Now()) {
			if i >= kbCompressMax || ctx.Err() != nil {
				break
			}
			if err := e.Compress(ctx, c); err != nil {
				h.logf("kb: compress %s: %v", c.ID, err)
			}
		}
		e.RefreshProjects(ctx, 3)
		if h.lark != nil && time.Since(lastItems) >= kbItemsPoll {
			lastItems = time.Now()
			if err := e.CollectItems(ctx, &larkItems{c: h.lark}); err != nil {
				h.logf("kb: items: %v", err)
			}
			if err := e.TagItems(ctx); err != nil {
				h.logf("kb: tag items: %v", err)
			}
		}
		if time.Since(lastPrune) >= 24*time.Hour {
			lastPrune = time.Now()
			if n := e.Store.Prune(time.Now(), textDays, attDays); n > 0 {
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
	if c.Topic {
		newer = ""
	}
	return out, newer, p.HasNewer, nil
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

type larkItems struct{ c *lark.Client }

func (s *larkItems) Items(ctx context.Context) ([]kb.Item, error) {
	var out []kb.Item
	now := time.Now().UTC()
	q := url.Values{"start": {now.AddDate(0, 0, -1).Format("2006-01-02T00:00:00Z")}, "end": {now.AddDate(0, 0, 8).Format("2006-01-02T00:00:00Z")}}
	if raw, err := s.c.Read(ctx, "GET", "calendar/week", q, nil); err == nil {
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
	if raw, err := s.c.Read(ctx, "GET", "meetings", url.Values{"count": {"30"}}, nil); err == nil {
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
	raw, err := s.c.Read(cctx, "GET", "documents/read", url.Values{"url": {u}, "format": {"markdown"}}, nil)
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

// --- assistant tools ---

var kbTools = []proto.Tool{
	{Name: "kb_search", Description: "Search the knowledge base (~/.jarvis/data): summaries and metadata of chats, people, projects, docs, " +
		"meetings, events, project timelines and, with messages=true, the full message archive (newest days first). All words must match.",
		InputSchema: schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"messages":{"type":"boolean","description":"also search the message archive (default true)"},"limit":{"type":"integer","description":"default 40"}}}`)},
	{Name: "kb_list", Description: "List knowledge-base entities of one kind ranked by importance (P0 first), relevance to the user, recency: " +
		"projects (short = a feature / launch, long = a team mission), chats, people, docs, meetings, events. Optional tag filter.",
		InputSchema: schema(`{"type":"object","required":["kind"],"properties":{"kind":{"type":"string","enum":["projects","chats","people","docs","meetings","events"]},"tag":{"type":"string"},"limit":{"type":"integer","description":"default 30"}}}`)},
	{Name: "kb_get", Description: "Read one entity: its metadata (links to people / chats / projects, tags), compressed context (summary.md), " +
		"a project's timeline, a chat's messages of the last 7 days. id from kb_list / kb_search (a project also by name).",
		InputSchema: schema(`{"type":"object","required":["kind","id"],"properties":{"kind":{"type":"string","enum":["projects","chats","people","docs","meetings","events"]},"id":{"type":"string"}}}`)},
	{Name: "kb_update", Description: "Write to the knowledge base: create or change a project (name, project_kind short|long, status, goal, " +
		"my_role, aliases, link chats / people, code dirs as \"<machine>:<path>\" so Agents there belong to it, append a timeline `log` line, " +
		"replace its summary), or tag / annotate a chat, person, doc, meeting, event (importance P0–P3, relevance 0–100, tags, note, link projects). " +
		"Tags set here are kept over the automatic ones. Use it whenever a conversation settles something about a project or person.",
		InputSchema: schema(`{"type":"object","required":["kind","id"],"properties":{"kind":{"type":"string","enum":["projects","chats","people","docs","meetings","events"]},"id":{"type":"string","description":"id, or a project's name (created when new)"},"name":{"type":"string"},"project_kind":{"type":"string","enum":["short","long"]},"status":{"type":"string","enum":["active","paused","done"]},"goal":{"type":"string"},"my_role":{"type":"string"},"note":{"type":"string"},"importance":{"type":"string","enum":["P0","P1","P2","P3"]},"relevance":{"type":"integer"},"tags":{"type":"array","items":{"type":"string"}},"summary":{"type":"string","description":"Markdown, replaces summary.md"},"log":{"type":"string","description":"one line appended to a project's timeline"},"aliases":{"type":"array","items":{"type":"string"}},"chats":{"type":"array","items":{"type":"string"}},"people":{"type":"array","items":{"type":"string"}},"dirs":{"type":"array","items":{"type":"string"}},"projects":{"type":"array","items":{"type":"string"}}}}`)},
}

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
func (h *Hub) kbContextFor(it *InboxItem) string {
	e := h.kb.eng
	if e == nil {
		return ""
	}
	var c *kb.Chat
	switch {
	case it.Lark != nil:
		c, _ = e.Store.Chat(kb.ChatID("lark", it.Lark.FeedID))
	case it.Mail != nil:
		c, _ = e.Store.Chat(kb.ChatID("mail", it.Mail.AccountID+"-"+strings.ToLower(it.Mail.From.Email)))
	}
	if c == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 知识库里的背景（~/.jarvis/data）\n\n")
	if sum := e.Store.Summary(kb.KindChat, c.ID); sum != "" {
		fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("会话档案 · "+c.Name), clipRunes(sum, 4000))
	}
	for i, slug := range c.Projects {
		if i >= 3 {
			break
		}
		if p, ok := e.Store.Project(slug); ok {
			fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("项目 · "+p.Name), clipRunes(e.Store.Summary(kb.KindProject, slug), 3000))
		}
	}
	fmt.Fprintf(&b, "\n会话 id：%s。更多用 kb_get / kb_search 查。处理完如果得出了新结论（项目进展、谁负责什么、截止时间），用 kb_update 写回对应项目的 log（这是允许的写操作）。\n", c.ID)
	return b.String()
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
		list := e.List(kind, r.URL.Query().Get("tag"))
		if list == nil {
			list = []kb.Entry{}
		}
		writeJSON(w, map[string]any{"items": list, "tags": e.TopTags(30)})
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
		hits := e.Search(r.URL.Query().Get("q"), r.URL.Query().Get("messages") != "0", 60)
		if hits == nil {
			hits = []kb.Hit{}
		}
		writeJSON(w, map[string]any{"hits": hits})
	})
}
