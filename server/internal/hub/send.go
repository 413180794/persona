package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Sending a message from the phone (docs/protocol.md「发消息」). The app sends each message with its own
// client_msg_id and resends what got no answer with the same id; the node types it into the agent's terminal. A
// message must arrive once: the hub remembers every id — in memory while a send runs, in Postgres after — so a retry
// is answered from what happened instead of typed again, also after the node reconnected (a new node process knows
// nothing of earlier sends) or the hub restarted. A send whose connection broke while it was being typed is
// "unknown": the retry is not typed, and the app asks the user to look before sending again (with a new id).

const (
	sendDelivered = "delivered"
	sendUnknown   = "unknown"
	sendSending   = "sending"
)

type sendLog struct {
	mu      sync.Mutex
	flights map[string]*sendFlight
}

// sendFlight is one client_msg_id's send: retries arriving while it runs wait for it and get the same answer.
type sendFlight struct {
	done   chan struct{}
	status int
	body   []byte
	at     time.Time
}

// join returns the flight for id and whether this request is the one to send it.
func (l *sendLog) join(id string) (*sendFlight, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.flights == nil {
		l.flights = map[string]*sendFlight{}
	}
	for k, f := range l.flights {
		if time.Since(f.at) > 10*time.Minute {
			delete(l.flights, k)
		}
	}
	if f := l.flights[id]; f != nil {
		return f, false
	}
	f := &sendFlight{done: make(chan struct{}), at: time.Now()}
	l.flights[id] = f
	return f, true
}

// forget drops an id whose send typed nothing, so a retry sends it.
func (l *sendLog) forget(id string, f *sendFlight) {
	l.mu.Lock()
	if l.flights[id] == f {
		delete(l.flights, id)
	}
	l.mu.Unlock()
}

// withoutControls drops what acts on a terminal rather than being read: line breaks and tabs stay.
func withoutControls(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return -1
		}
		return c
	}, s)
}

func (h *Hub) sendMessage(w http.ResponseWriter, r *http.Request, m *Machine) {
	p := map[string]any{}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&p); err != nil && !errors.Is(err, errEOF) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	sid := r.PathValue("sid")
	p["session_id"] = sid
	todoID, _ := p["todo_id"].(string)
	delete(p, "todo_id")
	if todoID != "" {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		t, err := h.getTodoForLink(r.Context(), todoID)
		if err != nil {
			writeAssistantErr(w, err) // 503 assistant_offline while the assistant is not connected
			return
		}
		if t == nil {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}
		if t.DoneAt != nil {
			writeErr(w, http.StatusConflict, errors.New("todo is already completed"))
			return
		}
		// Handed to the agent for the todo: the calls already made on it go first (#1618).
		if text, ok := p["text"].(string); ok {
			p["text"] = h.withTodoDecisions(r.Context(), todoID, text)
		}
	}
	if answerOnly(r) {
		// An answer-only device says something to a conversation it can see, no more. A message to an id nobody has starts
		// an agent on the computer; which program runs it, and whatever else a send can shape, are a full device's to pick;
		// and it is text for the agent, not keys for the terminal.
		s, ok := m.session(sid)
		if !ok || len(h.shown(m.cfg.ID, []proto.Session{s})) == 0 {
			writeErr(w, http.StatusNotFound, errors.New("no such session"))
			return
		}
		text, _ := p["text"].(string)
		text = withoutControls(text)
		if strings.TrimSpace(text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		said := map[string]any{"session_id": sid, "text": text}
		if id, ok := p["client_msg_id"]; ok {
			said["client_msg_id"] = id
		}
		p = said
	}
	if s, ok := m.session(sid); ok && isAssistant(s, m.assistantCwd()) {
		// The user is talking in it again: back in the list, and it works for them, not for a chain of conversations.
		h.asst().UserSpoke(r.Context(), assistantwire.SessionRef{MachineID: m.cfg.ID, SessionID: sid})
	}
	s, known := m.session(sid)
	if h.isMainOn(m.cfg.ID, sid) || (known && (s.Assistant || isAssistant(s, m.assistantCwd()))) {
		// One of the assistant's conversations: it says what the message runs on (and, the first one to a new main
		// conversation started empty, what the old one left before it).
		if err := h.assistantSend(r.Context(), m, sid, p); err != nil {
			writeAssistantErr(w, err)
			return
		}
	} else if l, _ := p["launcher"].(string); l == "" && known && resumesWithoutLauncher(s) {
		h.applyResumeLauncher(r.Context(), sid, p)
	}
	// The send carries on when the phone stops waiting (backgrounded, network gone): it may be typing already, and
	// its outcome is what the retry gets. Resuming an offline session can take a while (shell + claude startup).
	// One sent while the session still starts waits for claude to be ready (node/starting.go); one to a session that is
	// not running resumes it first. Either can wait minutes on a busy machine (node's claudeStartWait, #1396).
	wait := 90 * time.Second
	if s, ok := m.session(sid); ok && (s.Starting || s.Status == proto.StatusOffline) {
		wait = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), wait)
	defer cancel()
	id, _ := p["client_msg_id"].(string)
	if ch := channel.FromClientMsgID(id, []string{"voice"}); ch != "" {
		h.channelNoted(ch, m.cfg.ID, sid) // a voice conversation's errand (docs/channels.md「来源标记」)
	}
	external := false
	if s, ok := m.session(sid); ok {
		external = s.SourceKind != ""
	}
	if external && id == "" {
		callErr(w, &rpc.Error{Code: "codex_send_guard", Message: "发送到 app-server 必须带消息标识，避免断线后重复发送", Data: json.RawMessage(`{"delivered":"none"}`)})
		return
	}
	if id == "" {
		status, body := h.deliver(ctx, m, p)
		if status == http.StatusOK && todoID != "" {
			var title, cwd, agent string
			if s, ok := m.session(sid); ok {
				title, cwd, agent = s.Title, s.Cwd, s.Agent
			}
			if err := h.linkTodoAgent(ctx, todoID, m.cfg.ID, sid, title, cwd, agent, ""); err != nil {
				h.logf("sendMessage: link todo %s for %s/%s failed: %v", todoID, m.cfg.ID, sid, err)
			}
		}
		writeRaw(w, status, body)
		return
	}
	f, first := h.sends.join(id)
	if !first {
		select {
		case <-f.done:
			writeRaw(w, f.status, f.body)
		case <-r.Context().Done():
		}
		return
	}
	if external && h.store == nil {
		f.status, f.body = callErrBytes(&rpc.Error{Code: "codex_send_guard", Message: "无法记录消息标识，未向 app-server 发送", Data: json.RawMessage(`{"delivered":"none"}`)})
		close(f.done)
		h.sends.forget(id, f)
		writeRaw(w, f.status, f.body)
		return
	}
	state := ""
	if h.store != nil {
		var err error
		if state, err = h.store.ClaimSend(ctx, id, m.cfg.ID, sid); err != nil {
			if external {
				f.status, f.body = callErrBytes(&rpc.Error{Code: "codex_send_guard", Message: "无法记录消息标识，未向 app-server 发送", Data: json.RawMessage(`{"delivered":"none"}`)})
				close(f.done)
				h.sends.forget(id, f)
				writeRaw(w, f.status, f.body)
				return
			}
			h.logf("send %s: %v", id, err) // no record: the send still goes, without the guard
		}
	}
	switch state {
	case sendDelivered:
		f.status, f.body = http.StatusOK, []byte("{}\n")
	case sendSending, sendUnknown: // an earlier hub or connection was sending it and never learned the outcome
		f.status, f.body = callErrBytes(unknownDelivery("这条消息之前发送时连接断了"))
	default:
		var outcome string
		f.status, f.body, outcome = h.deliverOnce(ctx, m, p)
		if h.store != nil {
			if err := h.store.SettleSend(context.WithoutCancel(ctx), id, outcome); err != nil {
				h.logf("send %s: %v", id, err)
			}
		}
		if outcome == "" {
			defer h.sends.forget(id, f)
		}
	}
	close(f.done)
	if f.status == http.StatusOK && todoID != "" {
		var title, cwd, agent string
		if s, ok := m.session(sid); ok {
			title, cwd, agent = s.Title, s.Cwd, s.Agent
		}
		if err := h.linkTodoAgent(ctx, todoID, m.cfg.ID, sid, title, cwd, agent, ""); err != nil {
			h.logf("sendMessage: link todo %s for %s/%s failed: %v", todoID, m.cfg.ID, sid, err)
		}
	}
	writeRaw(w, f.status, f.body)
}

// deliverOnce asks the node to send and says what became of it: delivered, unknown (it may have been typed), or ""
// (nothing typed: refused, offline, failed before typing).
func (h *Hub) deliverOnce(ctx context.Context, m *Machine, p map[string]any) (int, []byte, string) {
	var out json.RawMessage
	err := m.call(ctx, "session.send", p, &out)
	switch outcome := sendOutcome(err); {
	case err == nil:
		if len(out) == 0 {
			out = json.RawMessage("{}")
		}
		return http.StatusOK, append(out, '\n'), outcome
	case outcome == sendUnknown && !hasDelivered(err):
		status, body := callErrBytes(unknownDelivery("发送途中和电脑的连接断了"))
		return status, body, outcome
	default:
		status, body := callErrBytes(err)
		return status, body, outcome
	}
}

func (h *Hub) deliver(ctx context.Context, m *Machine, p map[string]any) (int, []byte) {
	status, body, _ := h.deliverOnce(ctx, m, p)
	return status, body
}

// sendOutcome reads a node call's error: what may have reached the terminal.
func sendOutcome(err error) string {
	if err == nil {
		return sendDelivered
	}
	if errors.Is(err, errOffline) {
		return ""
	}
	var re *rpc.Error
	if errors.As(err, &re) { // the node answered: it marks failures after typing began
		var d struct {
			Delivered string `json:"delivered"`
		}
		_ = json.Unmarshal(re.Data, &d)
		if d.Delivered == sendUnknown {
			return sendUnknown
		}
		return ""
	}
	// No answer (the connection closed, the deadline passed): the node had the request and may have typed it.
	return sendUnknown
}

func hasDelivered(err error) bool {
	var re *rpc.Error
	return errors.As(err, &re) && bytes.Contains(re.Data, []byte(`"delivered"`))
}

func unknownDelivery(why string) error {
	return &rpc.Error{Message: why + "：它可能已经送到电脑上了，先看看对话里有没有，再决定要不要重发", Data: json.RawMessage(`{"delivered":"unknown"}`)}
}

// callErrBytes is callErr's response as bytes, so waiting retries can get the same one.
func callErrBytes(err error) (int, []byte) {
	rec := &bufferedResponse{header: http.Header{}}
	callErr(rec, err)
	return rec.status, rec.body.Bytes()
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *bufferedResponse) WriteHeader(code int)        { b.status = code }

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// resumesWithoutLauncher: a message to this session resumes it (it is not running) and nothing on its machine says
// which launcher it ran with (started outside Jarvis, no launcher matches its base URL and model). The node would
// fall back to plain claude, which may not be signed in there (node/sendsafe.go resumeLauncher).
func resumesWithoutLauncher(s proto.Session) bool {
	return s.Status == proto.StatusOffline && s.Launcher == "" && !s.Assistant && s.SourceKind == "" &&
		(s.Agent == "" || s.Agent == "claude")
}

// applyResumeLauncher adds 新建 Agent's launcher and model to the send: the ones the user picked for agents. When
// the purpose cannot run as a launcher the send goes as before (plain claude).
func (h *Hub) applyResumeLauncher(ctx context.Context, sid string, p map[string]any) {
	q := map[string]any{}
	if err := h.applyModel(ctx, "agent.new", q); err != nil {
		h.logf("resume %s: 新建 Agent's launcher not applied: %v", sid, err)
		return
	}
	for _, k := range []string{"launcher", "model", "effort", "model_fingerprint"} {
		if v, ok := q[k]; ok {
			p[k] = v
		}
	}
}
