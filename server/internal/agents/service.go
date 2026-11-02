package agents

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// Error is a refusal with the HTTP status the hub should answer.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func fail(status int, format string, args ...any) error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// HTTPStatus maps an error from the service to a status: its own refusals keep theirs, lark-connector's 4xx pass
// through (403 read-only token, 404, 409 account unavailable…), anything else is a bad gateway.
func HTTPStatus(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	var ae *lark.APIError
	if errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 && ae.Status != 401 {
		return ae.Status
	}
	return http.StatusBadGateway
}

// FollowUps are when to tell the apps to re-pull a session after a send: the bridge re-polls the agent at these
// delays itself (FOLLOW_UP_DELAYS_MS), and there is no streaming.
var FollowUps = []time.Duration{4 * time.Second, 15 * time.Second, 45 * time.Second}

// Service is the agents layer the hub serves: sources, a cache, the outbox, and change notifications.
type Service struct {
	sources  map[string]Source
	order    []string
	cache    *cache
	out      *outbox
	prefs    *prefsStore
	onChange func(ids []string) // sessions whose content changed: the apps re-pull them
	logf     func(string, ...any)

	pollMu sync.Mutex
	marks  map[string][2]int64 // id → rankTimeMs, unreadCount at the last poll
}

func NewService(kv KV, onChange func(ids []string), logf func(string, ...any), sources ...Source) *Service {
	s := &Service{sources: map[string]Source{}, cache: newCache(20 * time.Second), out: &outbox{kv: kv}, prefs: &prefsStore{kv: kv},
		onChange: onChange, logf: logf}
	if s.onChange == nil {
		s.onChange = func([]string) {}
	}
	if s.logf == nil {
		s.logf = func(string, ...any) {}
	}
	for _, src := range sources {
		s.sources[src.Name()] = src
		s.order = append(s.order, src.Name())
	}
	return s
}

func (s *Service) source(id string) (Source, string, error) {
	name, key, err := SplitID(id)
	if err != nil {
		return nil, "", &Error{Status: http.StatusBadRequest, Msg: err.Error()}
	}
	src := s.sources[name]
	if src == nil {
		return nil, "", fail(http.StatusNotFound, "agents: unknown source %q", name)
	}
	return src, key, nil
}

func (s *Service) Statuses(ctx context.Context) []Status {
	out := []Status{}
	for _, n := range s.order {
		st, _ := s.sources[n].Status(ctx)
		if st.Providers == nil {
			st.Providers = []lark.AgentProvider{}
		}
		if st.Accounts == nil {
			st.Accounts = []lark.AgentAccount{}
		}
		out = append(out, st)
	}
	return out
}

func (s *Service) sessionsOf(ctx context.Context, name string, fresh bool) ([]Session, error) {
	if !fresh {
		if list, ok := s.cache.getSessions(name); ok {
			return list, nil
		}
	}
	list, err := s.sources[name].Sessions(ctx)
	if err != nil {
		return nil, err
	}
	s.cache.putSessions(name, list)
	return list, nil
}

// Sessions lists sessions of every source (or one), optionally of one provider, ordered as the sources rank them.
func (s *Service) Sessions(ctx context.Context, source, provider string) ([]Session, error) {
	out := []Session{}
	for _, n := range s.order {
		if source != "" && n != source {
			continue
		}
		list, err := s.sessionsOf(ctx, n, false)
		if err != nil {
			return nil, err
		}
		for _, x := range list {
			if provider == "" || x.Provider == provider {
				out = append(out, x)
			}
		}
	}
	return out, nil
}

func (s *Service) Search(ctx context.Context, source, q string) ([]SearchHit, error) {
	out := []SearchHit{}
	for _, n := range s.order {
		if source != "" && n != source {
			continue
		}
		hits, err := s.sources[n].Search(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, hits...)
	}
	return out, nil
}

// Session finds one session by id, re-listing once if the cached list does not have it.
func (s *Service) Session(ctx context.Context, id string) (Session, error) {
	src, _, err := s.source(id)
	if err != nil {
		return Session{}, err
	}
	for _, fresh := range []bool{false, true} {
		list, err := s.sessionsOf(ctx, src.Name(), fresh)
		if err != nil {
			return Session{}, err
		}
		for _, x := range list {
			if x.ID == id {
				return x, nil
			}
		}
	}
	return Session{}, fail(http.StatusNotFound, "agents: no session %s", id)
}

func (s *Service) Messages(ctx context.Context, id, cursor string, limit int) (*MessagePage, error) {
	src, key, err := s.source(id)
	if err != nil {
		return nil, err
	}
	if cursor == "" {
		if p, ok := s.cache.getLatest(id, limit); ok {
			return p, nil
		}
	}
	p, err := src.Messages(ctx, key, cursor, limit)
	if err != nil {
		return nil, err
	}
	if p.Session != nil && !p.Session.HasAvatar {
		if list, ok := s.cache.getSessions(src.Name()); ok {
			for _, x := range list {
				if x.ID == id {
					cp := withAvatar(*p.Session, x.AvatarURL)
					p.Session = &cp
					break
				}
			}
		}
	}
	if cursor == "" {
		s.cache.putLatest(id, limit, p)
	}
	return p, nil
}

// Avatar decodes a session's avatar (404 when it has none).
func (s *Service) Avatar(ctx context.Context, id string) (*Avatar, error) {
	x, err := s.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	if x.AvatarURL == "" {
		return nil, fail(http.StatusNotFound, "agents: session %s has no avatar", id)
	}
	a, err := decodeAvatar(ctx, x.AvatarURL)
	if err != nil {
		return nil, fail(http.StatusBadGateway, "%v", err)
	}
	return a, nil
}

// SendResult is the outcome of a send: its outbox row, and whether it was already sent (nothing new went out).
type SendResult struct {
	Entry     OutboxEntry     `json:"outbox"`
	Duplicate bool            `json:"duplicate"`
	Upstream  json.RawMessage `json:"upstream,omitempty"`
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func NewClientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TextLen counts like JavaScript's String.length (UTF-16 units), which is what lark-connector's 20000 limit measures.
func TextLen(s string) int { return len(utf16.Encode([]rune(s))) }

func preview(s string) string {
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// Send posts text into a session as the user. clientID (a UUID; generated when empty) makes it idempotent: a
// clientId the outbox already saw sent is answered from the outbox without calling the source, and one that failed
// or never got an answer is retried with the same clientId, which lark-connector itself deduplicates.
func (s *Service) Send(ctx context.Context, id, clientID, text, origin string) (*SendResult, error) {
	src, key, err := s.source(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fail(http.StatusBadRequest, "agents: text is empty")
	}
	if n := TextLen(text); n > lark.AgentMaxText {
		return nil, fail(http.StatusRequestEntityTooLarge, "agents: text is %d characters, the limit is %d", n, lark.AgentMaxText)
	}
	if clientID == "" {
		clientID = NewClientID()
	} else if !uuidRe.MatchString(clientID) {
		return nil, fail(http.StatusBadRequest, "agents: clientId must be a UUID")
	}
	clientID = strings.ToLower(clientID)
	prev, seen := s.out.get(ctx, clientID)
	if seen && prev.SessionID != id {
		return nil, fail(http.StatusConflict, "agents: clientId %s was used for another session", clientID)
	}
	if seen && (prev.Status == OutSent || prev.Status == OutDeleted) {
		return &SendResult{Entry: prev, Duplicate: true}, nil
	}
	sess, err := s.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	if !sess.CanSend {
		return nil, fail(http.StatusConflict, "agents: session %s cannot send now (canSend=false)", id)
	}
	now := time.Now().UTC()
	e := prev
	if !seen {
		e = OutboxEntry{ClientID: clientID, SessionID: id, Preview: preview(text), Length: TextLen(text), Origin: origin, CreatedAt: now}
	}
	e.Status, e.Error, e.UpdatedAt = OutSending, "", now
	e.Attempts++
	if err := s.out.put(ctx, e); err != nil {
		s.logf("agents: outbox: %v", err)
	}
	up, err := src.Send(ctx, key, clientID, text)
	e.UpdatedAt = time.Now().UTC()
	if err != nil {
		e.Status, e.Error = OutFailed, err.Error()
	} else {
		e.Status = OutSent
	}
	if perr := s.out.put(ctx, e); perr != nil {
		s.logf("agents: outbox: %v", perr)
	}
	if err != nil {
		return nil, err
	}
	s.Updated(src.Name(), []string{key})
	s.followUp(src.Name(), key)
	return &SendResult{Entry: e, Upstream: up}, nil
}

func (s *Service) followUp(source, key string) {
	for _, d := range FollowUps {
		time.AfterFunc(d, func() { s.Updated(source, []string{key}) })
	}
}

func (s *Service) DeleteMessage(ctx context.Context, id, clientID string) error {
	src, key, err := s.source(id)
	if err != nil {
		return err
	}
	if err := src.DeleteMessage(ctx, key, clientID); err != nil {
		return err
	}
	if e, ok := s.out.get(ctx, strings.ToLower(clientID)); ok {
		e.Status, e.UpdatedAt = OutDeleted, time.Now().UTC()
		_ = s.out.put(ctx, e)
	}
	s.Updated(src.Name(), []string{key})
	return nil
}

func (s *Service) MarkRead(ctx context.Context, id string) error {
	src, key, err := s.source(id)
	if err != nil {
		return err
	}
	if err := src.MarkRead(ctx, key); err != nil {
		return err
	}
	s.Updated(src.Name(), []string{key})
	return nil
}

func (s *Service) Outbox(ctx context.Context, id string, limit int) []OutboxEntry {
	return s.out.list(ctx, id, limit)
}

// Updated is a source reporting that these sessions changed (agent-bridge.updated, a send, the poll): drop
// their cache and tell the apps. No keys means "something changed": only the list is dropped.
func (s *Service) Updated(source string, keys []string) {
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, source+":"+k)
	}
	s.cache.invalidate(source, ids)
	s.onChange(ids)
}

// Poll is the fallback when realtime events are missing: every interval it re-lists each source and reports the
// sessions whose rankTimeMs or unreadCount moved. The first pass only records a baseline.
func (s *Service) Poll(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		s.PollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) PollOnce(ctx context.Context) {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	first := s.marks == nil
	if first {
		s.marks = map[string][2]int64{}
	}
	for _, n := range s.order {
		list, err := s.sources[n].Sessions(ctx)
		if err != nil {
			continue // the next tick tries again; the routes report errors themselves
		}
		var changed []string
		for _, x := range list {
			m := [2]int64{x.RankTimeMs, int64(x.UnreadCount)}
			if old, ok := s.marks[x.ID]; !first && (!ok || old != m) {
				changed = append(changed, x.SessionKey)
			}
			s.marks[x.ID] = m
		}
		s.cache.putSessions(n, list)
		if len(changed) > 0 {
			s.Updated(n, changed)
		}
	}
}
