package kb

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SourceChat is a chat as a source lists it, newest activity first.
type SourceChat struct {
	ID        string
	Name      string
	Kind      string // dm | group | bot
	Topic     bool
	UpdatedAt time.Time
}

// SourceMessage is one message as a source returns it.
type SourceMessage struct {
	ID         string
	Position   string
	SenderID   string
	SenderName string
	Mine       bool
	Type       string // TEXT, POST, IMAGE, FILE, CARD, SYSTEM…
	Text       string
	At         time.Time
	Media      []string // source paths of images / files, for Download
}

// Source is where chats come from (Feishu through lark-connector).
type Source interface {
	Name() string // "lark"
	Chats(ctx context.Context, limit int) ([]SourceChat, error)
	// Messages returns messages newer than after (the latest page when after is ""), oldest first,
	// with the cursor to continue from.
	Messages(ctx context.Context, chat SourceChat, after string, limit int) (msgs []SourceMessage, newer string, more bool, err error)
	// Download saves one media path to dst.
	Download(ctx context.Context, media, dst string) error
}

// Engine runs the pipeline: archive, compress, evolve, tag, prune (docs/knowledge.md).
type Engine struct {
	Store   *Store
	Sources []Source
	Model   Model
	Owner   string // how the owner is called (the AI writes "我" for them)
	Logf    func(string, ...any)
	// Index mirrors an entity into the hub's Postgres index (optional).
	Index func(kind, id, name string, t Tagging, oneLine string, updated time.Time)

	// Limits (zero: defaults).
	ChatsPerRun  int // how many recently active chats an archive run looks at (100)
	FirstPage    int // how many messages a chat seen for the first time starts with (100)
	PagesPerChat int // pages read per chat and run (10)
	// AttachmentDays: attachments of older messages are not downloaded (they would be pruned anyway; 30).
	AttachmentDays int

	failed int // attachment downloads that failed this run (logged once per chat)
}

func (e *Engine) logf(f string, a ...any) {
	if e.Logf != nil {
		e.Logf("kb: "+f, a...)
	}
}

func (e *Engine) index(kind, id, name string, t Tagging, one string, updated time.Time) {
	if e.Index != nil {
		e.Index(kind, id, name, t, one, updated)
	}
}

func or(n, d int) int {
	if n <= 0 {
		return d
	}
	return n
}

// ChatID is the knowledge-base id of a source chat.
func ChatID(source, id string) string { return SafeID(source + "-" + id) }

// PersonID is the knowledge-base id of a sender.
func PersonID(source, id string) string { return SafeID(source + "-" + id) }

// Archive reads what is new in the recently active chats of every source and appends it to the day files.
// It returns the ids of chats that got new messages.
func (e *Engine) Archive(ctx context.Context) ([]string, error) {
	var changed []string
	for _, src := range e.Sources {
		chats, err := src.Chats(ctx, or(e.ChatsPerRun, 100))
		if err != nil {
			return changed, fmt.Errorf("%s chats: %w", src.Name(), err)
		}
		for _, sc := range chats {
			if ctx.Err() != nil {
				return changed, ctx.Err()
			}
			e.failed = 0
			n, err := e.archiveChat(ctx, src, sc)
			if e.failed > 0 {
				e.logf("%s: %d attachment(s) not downloaded", sc.Name, e.failed)
			}
			if err != nil {
				e.logf("archive %s %s: %v", src.Name(), sc.ID, err)
				continue
			}
			if n > 0 {
				changed = append(changed, ChatID(src.Name(), sc.ID))
			}
		}
	}
	return changed, nil
}

func (e *Engine) archiveChat(ctx context.Context, src Source, sc SourceChat) (int, error) {
	id := ChatID(src.Name(), sc.ID)
	c, ok := e.Store.Chat(id)
	if !ok {
		c = &Chat{ID: id, Source: src.Name(), SourceID: sc.ID, Kind: sc.Kind, Name: sc.Name, Created: time.Now().UTC()}
	}
	if sc.Name != "" {
		c.Name = sc.Name
	}
	if sc.Kind != "" {
		c.Kind = sc.Kind
	}
	// Nothing new since the last run (the list's time is the last message's).
	if ok && !sc.UpdatedAt.IsZero() && !sc.UpdatedAt.After(c.LastAt) {
		return 0, nil
	}
	cursor := c.Cursor
	limit := 100
	if cursor == "" {
		limit = or(e.FirstPage, 100)
	}
	total := 0
	for page := 0; page < or(e.PagesPerChat, 10); page++ {
		msgs, newer, more, err := src.Messages(ctx, sc, cursor, limit)
		if err != nil {
			return total, err
		}
		var lines []Line
		for _, m := range msgs {
			// Already archived: the cursor's own message, or (topic groups, read from the newest page each time) anything
			// not newer than the last archived message.
			if m.Position != "" && m.Position == cursor || c.Messages > 0 && !m.At.After(c.LastAt) {
				continue
			}
			l := e.line(ctx, src, c, m)
			if l.Text == "" {
				continue
			}
			lines = append(lines, l)
			e.notePerson(c, src.Name(), m)
			c.Mine = c.Mine || m.Mine
			if m.At.After(c.LastAt) {
				c.LastAt = m.At
			}
		}
		if err := e.Store.AppendMessages(c, lines); err != nil {
			return total, err
		}
		total += len(lines)
		if newer != "" {
			cursor = newer
		}
		// Topic groups only page backwards: one page per run, the newest.
		if !more || sc.Topic || newer == "" {
			break
		}
	}
	c.Cursor = cursor
	c.Messages += total
	c.Pending += total
	if c.LastAt.IsZero() {
		c.LastAt = sc.UpdatedAt
	}
	if err := e.Store.PutChat(c); err != nil {
		return total, err
	}
	e.index(KindChat, c.ID, c.Name, c.Tagging, c.OneLine, c.LastAt)
	return total, nil
}

var docLink = regexp.MustCompile(`https?://[\w.-]+\.(?:feishu\.cn|larksuite\.com|larkoffice\.com)/(?:docx|docs|wiki|sheets|base|mindnotes|slides)/[A-Za-z0-9]+`)

// line turns a message into an archive line; images and files are saved under attachments/<day>/.
func (e *Engine) line(ctx context.Context, src Source, c *Chat, m SourceMessage) Line {
	sender := m.SenderName
	if m.Mine {
		sender = "我"
	}
	if sender == "" {
		sender = "?"
	}
	text := strings.TrimSpace(m.Text)
	if m.Type == "SYSTEM" && text == "" {
		return Line{}
	}
	day := m.At.Local().Format("2006-01-02")
	if time.Since(m.At) > time.Duration(or(e.AttachmentDays, 30))*24*time.Hour {
		if len(m.Media) > 0 {
			text += " [附件]"
		}
		return Line{At: m.At, Sender: sender, Text: strings.TrimSpace(text)}
	}
	for i, media := range m.Media {
		name := fmt.Sprintf("%s-%d%s", SafeID(m.ID), i, extOf(media, m.Type))
		dst := e.Store.path(KindChat, c.ID, "attachments", day, name)
		if err := src.Download(ctx, media, dst); err != nil {
			e.failed++
			text += " [附件]"
			continue
		}
		rel, _ := filepath.Rel(e.Store.path(KindChat, c.ID), dst)
		text += fmt.Sprintf(" [附件](%s)", rel)
	}
	for _, u := range docLink.FindAllString(text, 5) {
		e.noteDoc(u, c.ID, m.At)
	}
	return Line{At: m.At, Sender: sender, Text: text}
}

func extOf(media, typ string) string {
	if ext := filepath.Ext(strings.SplitN(media, "?", 2)[0]); ext != "" && len(ext) <= 6 {
		return ext
	}
	if typ == "IMAGE" || typ == "POST" {
		return ".jpg"
	}
	return ""
}

// notePerson links the sender and the chat both ways.
func (e *Engine) notePerson(c *Chat, source string, m SourceMessage) {
	if m.Mine || m.SenderID == "" {
		return
	}
	pid := PersonID(source, m.SenderID)
	p, ok := e.Store.Person(pid)
	if !ok {
		p = &Person{ID: pid, Source: source, SourceID: m.SenderID, Name: m.SenderName}
	}
	if m.SenderName != "" {
		p.Name = m.SenderName
	}
	before, seen := len(p.Chats), p.LastSeen
	p.Chats = addUnique(p.Chats, c.ID)
	if m.At.After(p.LastSeen) {
		p.LastSeen = m.At
	}
	c.People = addUnique(c.People, pid)
	// Only write when something a reader cares about changed (not on every message of a busy group).
	if !ok || len(p.Chats) != before || p.LastSeen.Sub(seen) > time.Hour {
		if err := e.Store.PutPerson(p); err == nil {
			e.index(KindPerson, p.ID, p.Name, p.Tagging, p.Note, p.LastSeen)
		}
	}
}

// noteDoc remembers a document link seen in a chat.
func (e *Engine) noteDoc(u, chatID string, at time.Time) {
	id := SafeID(u[strings.LastIndex(u, "/")+1:])
	if _, ok := e.Store.Item(KindDoc, id); ok {
		return
	}
	_ = e.Store.PutItem(&Item{Kind: KindDoc, ID: id, URL: u, At: at, Where: chatID, Seen: time.Now().UTC()})
}

// Backfiller is a source that can read a chat backwards (history older than the first archive run).
type Backfiller interface {
	// Older returns messages before position before ("" = the newest page), oldest first, and the cursor to go further back.
	Older(ctx context.Context, chat SourceChat, before string, limit int) (msgs []SourceMessage, older string, more bool, err error)
}

// Backfill reads older history of the most relevant chats into their day files, back to the text retention:
// at most chats per run, pages per chat. The new lines are only archived (searchable), not compressed again.
func (e *Engine) Backfill(ctx context.Context, textDays, chats, pages int) (int, error) {
	limit := time.Now().AddDate(0, 0, -textDays)
	var todo []*Chat
	for _, c := range e.Store.Chats() {
		if !c.BackfillDone && c.Source != "mail" {
			todo = append(todo, c)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].Relevance > todo[j].Relevance })
	total := 0
	for i, c := range todo {
		if i >= chats || ctx.Err() != nil {
			break
		}
		var src Backfiller
		var name string
		for _, s := range e.Sources {
			if b, ok := s.(Backfiller); ok && s.Name() == c.Source {
				src, name = b, s.Name()
			}
		}
		if src == nil {
			continue
		}
		sc := SourceChat{ID: c.SourceID, Name: c.Name, Kind: c.Kind}
		first := e.Store.FirstAt(c.ID)
		cursor := c.Oldest
		for page := 0; page < pages; page++ {
			msgs, older, more, err := src.Older(ctx, sc, cursor, 100)
			if err != nil {
				e.logf("backfill %s: %v", c.Name, err)
				c.BackfillDone = true // do not retry every run; the recent text is there
				break
			}
			var lines []Line
			reached := false
			for _, m := range msgs {
				if m.At.Before(limit) {
					reached = true
					continue
				}
				// Only what is older than the oldest archived line (minute precision: equal minutes are already there).
				if !first.IsZero() && !m.At.Before(first.Truncate(time.Minute)) {
					continue
				}
				if l := e.line(ctx, sourceOf(e.Sources, name), c, m); l.Text != "" {
					lines = append(lines, l)
					e.notePerson(c, name, m)
				}
			}
			if err := e.Store.PrependMessages(c, lines); err != nil {
				return total, err
			}
			if len(lines) > 0 {
				first = lines[0].At
				for _, l := range lines {
					if l.At.Before(first) {
						first = l.At
					}
				}
			}
			total += len(lines)
			c.Messages += len(lines)
			if older != "" {
				cursor = older
			}
			if reached || !more || older == "" {
				c.BackfillDone = true
				break
			}
		}
		c.Oldest = cursor
		if err := e.Store.PutChat(c); err != nil {
			return total, err
		}
	}
	return total, nil
}

func sourceOf(list []Source, name string) Source {
	for _, s := range list {
		if s.Name() == name {
			return s
		}
	}
	return nil
}
