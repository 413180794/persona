package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant manages its own conversations (docs/assistant.md「管理自己的对话」, the user's ask on 2026-11-01):
// list, read, rename, archive, start one and message another. Archiving only hides a conversation (the app's 已归档
// lists it, nothing is deleted); the set is in the hub (settings assistant.archived), so Claude and Codex
// conversations are archived alike. Messages between conversations are queued in the hub while the receiver runs
// and carry where they come from; a chain of them is cut after a few hops and a few per pair in ten minutes, so two
// conversations can't keep each other going.

const (
	archivedKey = "assistant.archived"
	relayKey    = "assistant.relay"

	relayMaxHops   = 3                // hops from the user's own message
	relayPairMax   = 3                // messages from one conversation to another …
	relayFromMax   = 10               // … and from one conversation to any …
	relayWindow    = 10 * time.Minute // … in this long
	relayHopMemory = time.Hour        // a hop count older than this no longer counts (the user has moved on)
)

type convArchived struct {
	MachineID string    `json:"machine_id"`
	At        time.Time `json:"at"`
	By        string    `json:"by"` // user | assistant
}

type archiveBook struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]*convArchived // by session id
}

// archivedLocked reads the set once; b.mu is held. A failed read leaves it unloaded (nothing is saved over it).
func (h *Hub) archivedLocked(ctx context.Context) bool {
	b := &h.archived
	if b.loaded {
		return true
	}
	m := map[string]*convArchived{}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, archivedKey, &m); err != nil {
			h.logf("load archived conversations: %v", err)
			return false
		}
	}
	if m == nil {
		m = map[string]*convArchived{}
	}
	b.m, b.loaded = m, true
	return true
}

// archivedIDs are the archived conversations' ids.
func (h *Hub) archivedIDs() map[string]bool {
	b := &h.archived
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.archivedLocked(context.Background()) {
		return nil
	}
	out := make(map[string]bool, len(b.m))
	for id := range b.m {
		out[id] = true
	}
	return out
}

var errArchiveMain = errors.New("主对话不能归档")

// setArchived archives (on) or brings back a conversation; by is who did it. It reports whether anything changed.
func (h *Hub) setArchived(ctx context.Context, machine, id string, on bool, by string) (bool, error) {
	if on {
		if cfg := h.assistantConfig(ctx); cfg.Main == id {
			return false, errArchiveMain
		}
	}
	if h.store == nil {
		return false, errors.New("no database")
	}
	b := &h.archived
	b.mu.Lock()
	if !h.archivedLocked(ctx) {
		b.mu.Unlock()
		return false, errors.New("archived conversations could not be read")
	}
	_, was := b.m[id]
	if was == on {
		b.mu.Unlock()
		return false, nil
	}
	if on {
		b.m[id] = &convArchived{MachineID: machine, At: time.Now().UTC(), By: by}
	} else {
		delete(b.m, id)
	}
	err := h.store.PutSetting(ctx, archivedKey, b.m)
	if err != nil { // keep memory as the database has it
		if on {
			delete(b.m, id)
		} else {
			b.m[id] = &convArchived{MachineID: machine, At: time.Now().UTC(), By: by}
		}
	}
	b.mu.Unlock()
	if err != nil {
		return false, err
	}
	h.retitled(machine) // the apps get the list with the flag now
	return true, nil
}

// unarchiveOnSend brings an archived conversation back when something is sent to it (the user from the app,
// another conversation): what happens in it should be in the list.
func (h *Hub) unarchiveOnSend(machine, id string) bool {
	if !h.archivedIDs()[id] {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	changed, err := h.setArchived(ctx, machine, id, false, titleByUser)
	if err != nil {
		h.logf("unarchive %s: %v", short(id), err)
	}
	return changed
}

// archiveRoutes: POST /api/v1/assistant/conversations/{id}/archive and …/unarchive.
func (h *Hub) archiveRoutes(api *http.ServeMux) {
	for _, op := range []struct {
		path string
		on   bool
	}{{"archive", true}, {"unarchive", false}} {
		api.HandleFunc("POST /api/v1/assistant/conversations/{id}/"+op.path, func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if !validSessionID(id) {
				writeErr(w, http.StatusBadRequest, errors.New("invalid conversation id"))
				return
			}
			cfg := h.assistantConfig(r.Context())
			if _, err := h.setArchived(r.Context(), cfg.MachineID, id, op.on, titleByUser); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, errArchiveMain) {
					status = http.StatusBadRequest
				}
				writeErr(w, status, err)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "archived": op.on})
		})
	}
}

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func validSessionID(id string) bool { return sessionIDRe.MatchString(id) }

// --- messages between conversations ---

type relayMsg struct {
	From      string    `json:"from"`
	FromTitle string    `json:"from_title"`
	Tool      string    `json:"tool"`
	Text      string    `json:"text"`
	Hop       int       `json:"hop"`
	At        time.Time `json:"at"`
}

type relayHop struct {
	Hop int       `json:"hop"`
	At  time.Time `json:"at"`
}

// relayState is what is saved: the queued messages and how far each conversation's current work is from the user.
type relayState struct {
	Queue map[string][]relayMsg `json:"queue,omitempty"` // by receiver
	Hops  map[string]relayHop   `json:"hops,omitempty"`  // by conversation: the hop of the message it is working on
}

type relayBook struct {
	mu       sync.Mutex
	loaded   bool
	st       relayState
	sent     map[[2]string][]time.Time // (from, to) -> when; in memory: a restart only resets the rate
	draining map[string]bool
}

func (h *Hub) relayLocked(ctx context.Context) bool {
	b := &h.relays
	if b.loaded {
		return true
	}
	st := relayState{}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, relayKey, &st); err != nil {
			h.logf("load conversation relay: %v", err)
			return false
		}
	}
	if st.Queue == nil {
		st.Queue = map[string][]relayMsg{}
	}
	if st.Hops == nil {
		st.Hops = map[string]relayHop{}
	}
	b.st, b.loaded = st, true
	b.sent, b.draining = map[[2]string][]time.Time{}, map[string]bool{}
	return true
}

func (h *Hub) saveRelayLocked() {
	if h.store == nil {
		return
	}
	for id, hp := range h.relays.st.Hops {
		if time.Since(hp.At) > relayHopMemory {
			delete(h.relays.st.Hops, id)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, relayKey, h.relays.st); err != nil {
		h.logf("save conversation relay: %v", err)
	}
}

// hopOf is how many relayed messages away from the user's own the conversation's current work is (0: the user).
func (h *Hub) hopOf(id string) int {
	b := &h.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.relayLocked(context.Background()) {
		return 0
	}
	if hp, ok := b.st.Hops[id]; ok && time.Since(hp.At) <= relayHopMemory {
		return hp.Hop
	}
	return 0
}

// userSpoke: the user sent a message to the conversation, which works for them again (hop 0).
func (h *Hub) userSpoke(id string) {
	b := &h.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.relayLocked(context.Background()) {
		return
	}
	if _, ok := b.st.Hops[id]; ok {
		delete(b.st.Hops, id)
		h.saveRelayLocked()
	}
}

// relayAllowed checks the hop and rate limits for a message from → to and counts it when allowed.
func (h *Hub) relayAllowed(from, to string, hop int, now time.Time) error {
	if hop > relayMaxHops {
		return fmt.Errorf("not sent: this would be hop %d from the user's own message (at most %d). Tell the user what you have instead", hop, relayMaxHops)
	}
	b := &h.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.relayLocked(context.Background()) {
		return errors.New("the relay state could not be read")
	}
	pair, all := 0, 0
	for k, ts := range b.sent {
		kept := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < relayWindow {
				kept = append(kept, t)
			}
		}
		b.sent[k] = kept
		if k[0] == from {
			all += len(kept)
			if k[1] == to {
				pair += len(kept)
			}
		}
	}
	if pair >= relayPairMax {
		return fmt.Errorf("not sent: %d messages to that conversation in the last %d minutes already (at most %d). Wait for its answer", pair, int(relayWindow.Minutes()), relayPairMax)
	}
	if all >= relayFromMax {
		return fmt.Errorf("not sent: %d messages to other conversations in the last %d minutes already (at most %d)", all, int(relayWindow.Minutes()), relayFromMax)
	}
	b.sent[[2]string{from, to}] = append(b.sent[[2]string{from, to}], now)
	return nil
}

// relayText is the message as the receiver gets it (prompts/assistant/relay.md).
func relayText(msg relayMsg) string {
	return prompts.RenderRelay(prompts.Relay{FromID: msg.From, FromTitle: msg.FromTitle, Tool: msg.Tool, Hop: msg.Hop,
		MaxHops: relayMaxHops, Text: msg.Text})
}

// queueRelay keeps a message for a running conversation until its turn ends.
func (h *Hub) queueRelay(to string, msg relayMsg) int {
	b := &h.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.relayLocked(context.Background()) {
		return 0
	}
	b.st.Queue[to] = append(b.st.Queue[to], msg)
	h.saveRelayLocked()
	return len(b.st.Queue[to])
}

// queuedRelays are the texts waiting for a conversation (shown above its composer, like typed-ahead input).
func (h *Hub) queuedRelays() map[string][]string {
	b := &h.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.relayLocked(context.Background()) || len(b.st.Queue) == 0 {
		return nil
	}
	out := map[string][]string{}
	for id, q := range b.st.Queue {
		for _, msg := range q {
			out[id] = append(out[id], "来自「"+msg.FromTitle+"」："+msg.Text)
		}
	}
	return out
}

// deliverRelay sends one message into an assistant conversation and notes the hop it is working on.
func (h *Hub) deliverRelay(ctx context.Context, m *Machine, to string, msg relayMsg) error {
	if err := m.call(ctx, "assistant.send", map[string]any{"session_id": to, "text": relayText(msg), "resume_only": true}, nil); err != nil {
		return err
	}
	b := &h.relays
	b.mu.Lock()
	if h.relayLocked(ctx) {
		b.st.Hops[to] = relayHop{Hop: msg.Hop, At: time.Now().UTC()}
		h.saveRelayLocked()
	}
	b.mu.Unlock()
	return nil
}

// drainRelays delivers the next queued message to every conversation of m that is not running (called whenever m's
// list changes: a turn that just ended, or the hub starting with messages still queued).
func (h *Hub) drainRelays(m *Machine, list []proto.Session) {
	b := &h.relays
	b.mu.Lock()
	if !h.relayLocked(context.Background()) || len(b.st.Queue) == 0 {
		b.mu.Unlock()
		return
	}
	var due []string
	for _, s := range list {
		if len(b.st.Queue[s.SessionID]) > 0 && s.Status != proto.StatusWorking && !s.Starting && !b.draining[s.SessionID] {
			b.draining[s.SessionID] = true
			due = append(due, s.SessionID)
		}
	}
	b.mu.Unlock()
	for _, id := range due {
		go func(id string) {
			b.mu.Lock()
			q := b.st.Queue[id]
			if len(q) == 0 {
				delete(b.draining, id)
				b.mu.Unlock()
				return
			}
			msg := q[0]
			b.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			err := h.deliverRelay(ctx, m, id, msg)
			cancel()
			b.mu.Lock()
			delete(b.draining, id)
			// Delivered, or the conversation is gone for good: off the queue. Another failure (offline) keeps it.
			if err == nil || strings.Contains(err.Error(), "is gone") {
				if q := b.st.Queue[id]; len(q) > 0 {
					b.st.Queue[id] = q[1:]
				}
				if len(b.st.Queue[id]) == 0 {
					delete(b.st.Queue, id)
				}
				h.saveRelayLocked()
			}
			b.mu.Unlock()
			if err != nil {
				h.logf("relay to %s: %v", short(id), err)
			}
		}(id)
	}
}

// --- the tools (definitions in prompts/mcp/assistant) ---

// assistantConversation finds an assistant conversation by full id or prefix on the assistant's machine.
func (h *Hub) assistantConversation(ctx context.Context, id string) (*Machine, proto.Session, error) {
	cfg := h.assistantConfig(ctx)
	m := h.localMachine(cfg.MachineID)
	if m == nil {
		return nil, proto.Session{}, errors.New("the assistant's machine is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, proto.Session{}, errors.New("id is required")
	}
	var found []proto.Session
	for _, s := range h.decorated(m) {
		if !s.Assistant {
			continue
		}
		if s.SessionID == id {
			return m, s, nil
		}
		if len(id) >= 8 && strings.HasPrefix(s.SessionID, id) {
			found = append(found, s)
		}
	}
	switch len(found) {
	case 1:
		return m, found[0], nil
	case 0:
		return nil, proto.Session{}, fmt.Errorf("no conversation %q (conversations_list has the ids)", id)
	}
	return nil, proto.Session{}, fmt.Errorf("%q is the start of %d conversations: give more of the id", id, len(found))
}

// conversationName is how a conversation is called in tool results and relayed messages.
func conversationName(s proto.Session, main string) string {
	if s.SessionID == main {
		return "主对话"
	}
	// Not named yet: the node's title is then the directory's name ("assistant") or Claude Code's default.
	if t := strings.TrimSpace(s.Title); t != "" && t != "Claude Code" && t != path.Base(s.Cwd) {
		return t
	}
	return "新对话"
}

func (h *Hub) toolConversationsList(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Query           string `json:"query"`
		IncludeArchived bool   `json:"include_archived"`
		Limit           int    `json:"limit"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Limit <= 0 {
		a.Limit = 30
	}
	a.Limit = min(a.Limit, 100)
	v := h.assistantState(ctx, a.IncludeArchived)
	list := v.Conversations
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	q := strings.ToLower(strings.TrimSpace(a.Query))
	loc := h.userLocation()
	var b strings.Builder
	n := 0
	for _, s := range list {
		name := conversationName(s, v.Main)
		if q != "" && !strings.Contains(strings.ToLower(name+"\n"+s.LastText), q) {
			continue
		}
		if n >= a.Limit {
			n++
			continue
		}
		n++
		var tags []string
		if s.SessionID == v.Main {
			tags = append(tags, "main")
		}
		if s.SessionID == self {
			tags = append(tags, "this conversation")
		}
		if s.Status == proto.StatusWorking || s.Starting {
			tags = append(tags, "running")
		}
		if s.Archived {
			tags = append(tags, "archived")
		}
		if s.Agent == "codex" {
			tags = append(tags, "codex")
		}
		fmt.Fprintf(&b, "- %s 「%s」", s.SessionID, name)
		if len(tags) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(tags, ", "))
		}
		if !s.UpdatedAt.IsZero() {
			fmt.Fprintf(&b, " · last active %s (%s)", s.UpdatedAt.In(loc).Format("01-02 15:04"), ago(s.UpdatedAt))
		}
		if t := oneLine(s.LastText); t != "" {
			fmt.Fprintf(&b, "\n  last: %s", clip(t, 160))
		}
		b.WriteString("\n")
	}
	if n == 0 {
		if q != "" {
			return fmt.Sprintf("No conversation matches %q.", a.Query), nil
		}
		return "No conversations.", nil
	}
	head := fmt.Sprintf("%d conversation(s), newest activity first", n)
	if !a.IncludeArchived {
		head += " (archived ones left out)"
	}
	if n > a.Limit {
		head += fmt.Sprintf("; the first %d", a.Limit)
	}
	return head + ":\n" + b.String(), nil
}

func (h *Hub) toolConversationRead(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		ID    string `json:"id"`
		Turns int    `json:"turns"`
	}
	_ = json.Unmarshal(args, &a)
	m, s, err := h.assistantConversation(ctx, a.ID)
	if err != nil {
		return "", err
	}
	if a.Turns <= 0 {
		a.Turns = 2
	}
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := m.call(ctx, "session.items", map[string]any{"session_id": s.SessionID, "turns": min(a.Turns, 10)}, &res); err != nil {
		return "", err
	}
	head := fmt.Sprintf("「%s」 %s (%s)", conversationName(s, h.assistantConfig(ctx).Main), s.SessionID, stateNames[s.State])
	return transcriptText(head, res.Items), nil
}

func (h *Hub) toolConversationRename(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		Topic     string `json:"topic"`
		UserAsked bool   `json:"user_asked"`
	}
	_ = json.Unmarshal(args, &a)
	m, s, err := h.assistantConversation(ctx, a.ID)
	if err != nil {
		return "", err
	}
	if s.SessionID == h.assistantConfig(ctx).Main {
		return "", errors.New("the main conversation is always shown as 主对话: it has no title to change")
	}
	if !slices.Contains(titleKinds, a.Kind) {
		return "", fmt.Errorf("kind must be one of %s", strings.Join(titleKinds, " / "))
	}
	topic := titleTopic(a.Topic)
	if topic == "" {
		return "", errors.New("topic is empty")
	}
	by, started := h.titleOf(s.SessionID)
	if by == titleByUser && !a.UserAsked {
		return "", fmt.Errorf("the user named this conversation 「%s」 themselves: rename it only when they ask you to, and then pass user_asked", s.Title)
	}
	if started.IsZero() { // started before the hub kept titles: today's day is the best guess
		started = time.Now()
	}
	title := titleDay(started, h.userLocation()) + " | " + a.Kind + " | " + topic
	who := titleByAssistant
	if a.UserAsked {
		who = titleByUser // the user's choice, said in words: it stays like one typed in the app
	}
	if err := h.renameConversation(ctx, m, s, title, who); err != nil {
		return "", err
	}
	if s.SessionID == self {
		return fmt.Sprintf("Renamed this conversation to 「%s」.", title), nil
	}
	return fmt.Sprintf("Renamed %s to 「%s」.", s.SessionID, title), nil
}

func (h *Hub) toolConversationArchive(ctx context.Context, self string, args json.RawMessage, on bool) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &a)
	m, s, err := h.assistantConversation(ctx, a.ID)
	if err != nil {
		if !on { // an archived conversation may be long out of the node's list: the id alone will do
			if id := strings.TrimSpace(a.ID); h.archivedIDs()[id] {
				_, err := h.setArchived(ctx, h.assistantConfig(ctx).MachineID, id, false, titleByAssistant)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Unarchived %s.", id), nil
			}
		}
		return "", err
	}
	name := conversationName(s, h.assistantConfig(ctx).Main)
	changed, err := h.setArchived(ctx, m.cfg.ID, s.SessionID, on, titleByAssistant)
	if err != nil {
		return "", err
	}
	switch {
	case !changed && on:
		return fmt.Sprintf("「%s」 was archived already.", name), nil
	case !changed:
		return fmt.Sprintf("「%s」 is not archived.", name), nil
	case !on:
		return fmt.Sprintf("Unarchived 「%s」: it is back in the list.", name), nil
	case s.SessionID == self:
		return "Archived this conversation: it is out of the list (under 已归档). This turn finishes as usual; a new message from the user brings it back.", nil
	}
	return fmt.Sprintf("Archived 「%s」 (under 已归档 in the app; nothing is deleted).", name), nil
}

func (h *Hub) toolConversationNew(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &a)
	if strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is empty")
	}
	from, hop, err := h.relayFrom(ctx, self)
	if err != nil {
		return "", err
	}
	if err := h.relayAllowed(self, "new", hop, time.Now()); err != nil {
		return "", err
	}
	msg := relayMsg{From: self, FromTitle: from, Tool: "conversation_new", Text: a.Text, Hop: hop, At: time.Now().UTC()}
	sctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	s, err := h.startAssistant(sctx, relayText(msg), nil, "", "", "")
	if err != nil {
		return "", err
	}
	h.titleNew(s.MachineID, s.SessionID, time.Now(), "")
	b := &h.relays
	b.mu.Lock()
	if h.relayLocked(ctx) {
		b.st.Hops[s.SessionID] = relayHop{Hop: hop, At: time.Now().UTC()}
		h.saveRelayLocked()
	}
	b.mu.Unlock()
	return fmt.Sprintf("Started conversation %s; it is working on the message now and is named after its first turn. It shows up in the app's sidebar.", s.SessionID), nil
}

func (h *Hub) toolConversationSend(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &a)
	if strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is empty")
	}
	m, s, err := h.assistantConversation(ctx, a.ID)
	if err != nil {
		return "", err
	}
	if s.SessionID == self {
		return "", errors.New("that is this conversation: you can't send a message to yourself")
	}
	from, hop, err := h.relayFrom(ctx, self)
	if err != nil {
		return "", err
	}
	if err := h.relayAllowed(self, s.SessionID, hop, time.Now()); err != nil {
		return "", err
	}
	msg := relayMsg{From: self, FromTitle: from, Tool: "conversation_send", Text: a.Text, Hop: hop, At: time.Now().UTC()}
	name := conversationName(s, h.assistantConfig(ctx).Main)
	back := ""
	if h.unarchiveOnSend(m.cfg.ID, s.SessionID) {
		back = " It was archived; it is back in the list."
	}
	if s.Status == proto.StatusWorking || s.Starting || h.queuedFor(s.SessionID) > 0 {
		n := h.queueRelay(s.SessionID, msg)
		h.sessionsChanged(m) // the receiver shows it waiting above its composer
		return fmt.Sprintf("「%s」 is running: the message is queued (%d waiting) and goes in when its current turn ends.%s", name, n, back), nil
	}
	if err := h.deliverRelay(ctx, m, s.SessionID, msg); err != nil {
		return "", err
	}
	return fmt.Sprintf("Sent to 「%s」 (%s).%s", name, s.SessionID, back), nil
}

// relayFrom is the sending conversation's name and the hop its message would be.
func (h *Hub) relayFrom(ctx context.Context, self string) (string, int, error) {
	if self == "" {
		return "", 0, errors.New("only a Jarvis conversation can do this")
	}
	name := "另一段对话"
	if _, s, err := h.assistantConversation(ctx, self); err == nil {
		name = conversationName(s, h.assistantConfig(ctx).Main)
	}
	return name, h.hopOf(self) + 1, nil
}

func (h *Hub) queuedFor(id string) int {
	b := &h.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.relayLocked(context.Background()) {
		return 0
	}
	return len(b.st.Queue[id])
}
