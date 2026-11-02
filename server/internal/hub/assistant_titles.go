package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Assistant conversations are titled 「MMDD | 类型 | 主题」 (docs/assistant.md「对话标题」, the user's ask on 2026-11-01):
// MMDD is the day the conversation started in the user's time zone, 类型 one of titleKinds, 主题 at most 14
// characters. A conversation the hub starts is noted (titleNew); when its first turn ends (titleAfterTurn) the
// small-task model reads that exchange and names it, off the conversation's path: nothing waits for it and the
// assistant isn't asked to do anything. A scheduled task's conversation is 「MMDD | 定时 | 任务名」 from the start,
// with no model call; it stays the same however many runs go into it.
//
// The titles live in the hub (settings assistant.titles) and are put on the session lists the apps get
// (decorated), so Claude and Codex conversations are named the same way and nothing is written into transcripts.
// A conversation is named once: a rename in the app (POST …/rename with a title) is recorded as the user's and
// is never replaced; conversations from before this are left as they were.

var titleKinds = []string{"功能", "设计", "修复", "优化", "发布", "探索", "文档", "研究", "评审", "定时"}

const (
	maxTitleTopic = 14 // characters
	titlesKey     = "assistant.titles"
	titleTask     = "定时"
	titleFallback = "探索"
)

// Who named a conversation.
const (
	titlePending = "pending" // started by the hub, named when its first turn ends
	titleAuto    = "auto"
	titleByTask  = "task"
	titleByUser  = "user"
	// The assistant renamed it (conversation_rename): not replaced by the automatic title, the user's rename wins.
	titleByAssistant = "assistant"
)

type convTitle struct {
	MachineID string    `json:"machine_id"`
	Title     string    `json:"title,omitempty"`
	By        string    `json:"by"`
	Started   time.Time `json:"started"`
}

type titleBook struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]*convTitle // by session id
	naming map[string]bool       // a model call is out for these
}

// titlesLocked reads the book once; b.mu is held. A failed read leaves it unloaded (nothing is saved over it).
func (h *Hub) titlesLocked(ctx context.Context) bool {
	b := &h.titles
	if b.loaded {
		return true
	}
	m := map[string]*convTitle{}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, titlesKey, &m); err != nil {
			h.logf("load assistant titles: %v", err)
			return false
		}
	}
	if m == nil {
		m = map[string]*convTitle{}
	}
	b.m, b.loaded = m, true
	return true
}

func (h *Hub) saveTitlesLocked() {
	if h.store == nil {
		return
	}
	// A conversation that never finished its first turn in a day won't: forget it.
	for id, t := range h.titles.m {
		if t.By == titlePending && time.Since(t.Started) > 24*time.Hour {
			delete(h.titles.m, id)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, titlesKey, h.titles.m); err != nil {
		h.logf("save assistant titles: %v", err)
	}
}

// titleNew notes a conversation the hub just started. task: the scheduled task's name when a task started it (the
// title is set now); "" to name it after its first turn.
func (h *Hub) titleNew(machine, id string, started time.Time, task string) {
	if id == "" {
		return
	}
	t := &convTitle{MachineID: machine, By: titlePending, Started: started.UTC()}
	if task != "" {
		t.Title, t.By = titleDay(started, h.userLocation())+" | "+titleTask+" | "+titleTopic(task), titleByTask
	}
	b := &h.titles
	b.mu.Lock()
	if !h.titlesLocked(context.Background()) {
		b.mu.Unlock()
		return
	}
	if _, ok := b.m[id]; !ok {
		b.m[id] = t
		h.saveTitlesLocked()
	}
	b.mu.Unlock()
	if task != "" {
		h.retitled(machine)
	}
}

// titleRenamed records the user's own title (the app's 重命名) so it is shown as it is and never replaced.
func (h *Hub) titleRenamed(machine, id, title string) { h.titleSet(machine, id, title, titleByUser) }

// titleOf is who named a conversation ("" when the hub never noted it) and when it started.
func (h *Hub) titleOf(id string) (by string, started time.Time) {
	b := &h.titles
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.titlesLocked(context.Background()) {
		return "", time.Time{}
	}
	if t := b.m[id]; t != nil {
		return t.By, t.Started
	}
	return "", time.Time{}
}

// titleSet records a title someone gave (by: user or assistant); a pending automatic one is not made any more.
func (h *Hub) titleSet(machine, id, title, by string) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return
	}
	b := &h.titles
	b.mu.Lock()
	if !h.titlesLocked(context.Background()) {
		b.mu.Unlock()
		return
	}
	t := b.m[id]
	if t == nil {
		t = &convTitle{MachineID: machine}
		b.m[id] = t
	}
	t.Title, t.By = title, by
	h.saveTitlesLocked()
	b.mu.Unlock()
	h.retitled(machine)
}

// renameSession is POST /api/v1/sessions/{mid}/{sid}/rename ({"title", "tab_name"}): the node renames the tab and /
// or the session; an assistant conversation's new title is also the user's from now on (titleRenamed).
func (h *Hub) renameSession(w http.ResponseWriter, r *http.Request, m *Machine) {
	p := map[string]any{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil && !errors.Is(err, errEOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sid := r.PathValue("sid")
	p["session_id"] = sid
	title, _ := p["title"].(string)
	s, _ := m.session(sid)
	assistant := isAssistant(s.Cwd, m.assistantCwd())
	if assistant && s.Agent == "codex" {
		delete(p, "title") // only the hub's title for Codex (renameConversation)
	}
	out := json.RawMessage(`{"ok":true}`)
	if p["title"] != nil || p["tab_name"] != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := m.call(ctx, "session.rename", p, &out); err != nil {
			callErr(w, err)
			return
		}
	}
	if assistant && strings.TrimSpace(title) != "" {
		h.titleRenamed(m.cfg.ID, sid, title)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

// renameConversation names an assistant conversation for the assistant's conversation_rename: like the app's 重命名,
// Claude's transcript gets the title too (its own lists show it); a Codex conversation's title is only the hub's
// (the node would write Claude's custom-title line into the Codex rollout).
func (h *Hub) renameConversation(ctx context.Context, m *Machine, s proto.Session, title, by string) error {
	if s.Agent != "codex" {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := m.call(cctx, "session.rename", map[string]any{"session_id": s.SessionID, "title": title}, nil); err != nil {
			return err
		}
	}
	h.titleSet(m.cfg.ID, s.SessionID, title, by)
	return nil
}

// conversationTitles are the titles to show, by session id.
func (h *Hub) conversationTitles() map[string]string {
	b := &h.titles
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.titlesLocked(context.Background()) {
		return nil
	}
	out := make(map[string]string, len(b.m))
	for id, t := range b.m {
		if t.Title != "" {
			out[id] = t.Title
		}
	}
	return out
}

// titleAfterTurn runs when an assistant conversation's turn ends: a conversation the hub started and nobody has
// named yet is named now, in the background.
func (h *Hub) titleAfterTurn(m *Machine, id string) {
	b := &h.titles
	b.mu.Lock()
	if !h.titlesLocked(context.Background()) {
		b.mu.Unlock()
		return
	}
	t := b.m[id]
	if t == nil || t.By != titlePending || b.naming[id] {
		b.mu.Unlock()
		return
	}
	if b.naming == nil {
		b.naming = map[string]bool{}
	}
	b.naming[id] = true
	started := t.Started
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	first, reply := h.firstExchange(ctx, m, id)
	title := h.makeTitle(ctx, started, first, reply)

	b.mu.Lock()
	delete(b.naming, id)
	if t := b.m[id]; t != nil && t.By == titlePending { // the user may have renamed it meanwhile
		t.Title, t.By = title, titleAuto
		h.saveTitlesLocked()
	}
	b.mu.Unlock()
	h.logf("assistant conversation %s: titled %q", short(id), title)
	h.retitled(m.cfg.ID)
}

// retitled pushes the machine's list again so the apps show the new title now.
func (h *Hub) retitled(machine string) {
	if m := h.byID[machine]; m != nil {
		h.sessionsChanged(m)
	}
}

// firstExchange is the conversation's first message and the answer to it (it has had one turn).
func (h *Hub) firstExchange(ctx context.Context, m *Machine, id string) (first, reply string) {
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := m.call(ctx, "session.items", map[string]any{"session_id": id, "turns": 1}, &res); err != nil {
		h.logf("title %s: %v", short(id), err)
	}
	for _, it := range res.Items {
		if it.Kind != proto.KindText || strings.TrimSpace(it.Text) == "" {
			continue
		}
		switch {
		case it.Role == "user" && first == "":
			first = it.Text
		case it.Role == "assistant":
			reply = it.Text
		}
	}
	if first == "" { // the list has the first message as the title of a conversation nobody named
		if s, ok := m.session(id); ok {
			first = s.Title
		}
	}
	// A conversation another one started (conversation_new) opens with a note on where it comes from: not its topic.
	first = strings.TrimSpace(contextBlock.ReplaceAllString(first, ""))
	return cutRunes(first, 1500), cutRunes(reply, 1500)
}

// makeTitle asks the model for 「类型 | 主题」, once more when that breaks the format, and falls back to
// 「探索 | the first message's first 14 characters」.
func (h *Hub) makeTitle(ctx context.Context, started time.Time, first, reply string) string {
	day := titleDay(started, h.userLocation())
	if strings.TrimSpace(first) != "" {
		retry := ""
		for range 2 {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			out, err := h.purposeModel("assistant.title").Complete(cctx, prompts.RenderSessionTitle(),
				prompts.RenderSessionTitleInput(prompts.SessionTitleInput{First: first, Reply: reply, Retry: retry}), 200)
			cancel()
			if err != nil {
				h.logf("title: %v", err)
				break
			}
			kind, topic, ok := parseTitle(out)
			if ok {
				return day + " | " + kind + " | " + topic
			}
			if kind != "" && retry != "" { // a known kind and a topic that is still too long: cut it
				return day + " | " + kind + " | " + titleTopic(topic)
			}
			retry = cutRunes(oneLine(out), 60)
		}
	}
	topic := titleTopic(first)
	if topic == "" {
		topic = "新对话"
	}
	return day + " | " + titleFallback + " | " + topic
}

// titleDay is MMDD of t in the user's time zone (the hub runs in UTC: 00:30 in Shanghai is the day before there).
func titleDay(t time.Time, loc *time.Location) string { return t.In(loc).Format("0102") }

var titleDate = regexp.MustCompile(`^\d{4}\s*$`)

// parseTitle reads 「类型 | 主题」 from what the model said (one line; a date it added anyway, quotes, a full-width
// bar are tolerated). kind is "" when it isn't one of titleKinds; ok when the topic fits too.
func parseTitle(out string) (kind, topic string, ok bool) {
	line := ""
	for l := range strings.SplitSeq(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	line = strings.Trim(line, "「」\"'“”` ")
	parts := strings.FieldsFunc(line, func(r rune) bool { return r == '|' || r == '｜' })
	for len(parts) > 2 && titleDate.MatchString(strings.TrimSpace(parts[0])) {
		parts = parts[1:]
	}
	if len(parts) != 2 {
		return "", "", false
	}
	kind = strings.TrimSpace(parts[0])
	if !slices.Contains(titleKinds, kind) {
		kind = ""
	}
	topic = strings.Trim(strings.Join(strings.Fields(parts[1]), " "), "「」\"'“”。.，, ")
	n := utf8.RuneCountInString(topic)
	return kind, topic, kind != "" && n > 0 && n <= maxTitleTopic
}

var contextBlock = regexp.MustCompile(`(?s)<jarvis-context\b[^>]*>.*?</jarvis-context>`)

var titleNoise = regexp.MustCompile(`(?s)<[a-zA-Z][\w-]*[^>]*>.*?</[a-zA-Z][\w-]*>|[⏰#*>` + "`" + `]`)

// titleTopic is at most maxTitleTopic characters of s on one line: a task's name, or the fallback from the first
// message (without the tags and markdown around what the user wrote).
func titleTopic(s string) string {
	s = strings.Join(strings.Fields(titleNoise.ReplaceAllString(s, " ")), " ")
	s = strings.Trim(s, "，。,.：:；; ")
	return cutRunes(s, maxTitleTopic)
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return s
}
