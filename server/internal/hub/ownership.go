package hub

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
)

// Ownership (docs/managers.md「归属」): everything belongs to one manager, and the hub sends what concerns a thing to
// the manager it belongs to. This file is the one place that says who that is, so moving a machine or a source to
// another manager (step 3) changes ownership here and nowhere else:
//
//   - a machine: the manager that reports it (its devices.state); before any did, the one that carries hub.json's
//     machines (devicesOwner);
//   - a session: its machine's manager, unless it is one of a manager's own conversations (its snapshot lists it);
//   - an interaction: the manager that reported it;
//   - what names no thing the hub can place (the assistant's pages, the inbox, the knowledge base, the model
//     configuration, the todos, the extensions and sources…): the manager the request names (ManagerHeader, or the
//     query parameter ManagerQuery), else the default manager. An item of such a list (an inbox item, an extension)
//     is reached by naming the manager its list stamped on it.
//
// A request naming a manager the roster does not have is answered 404 no_manager; one whose manager is not connected
// 503 assistant_offline, as for the one assistant before.

const (
	// ManagerHeader names the manager a request is for (an id from GET /api/v1/managers).
	ManagerHeader = "X-Persona-Manager"
	// ManagerQuery is the same as a query parameter (a WebSocket, a link opened in a browser).
	ManagerQuery = "manager"
)

// errUnknownManager: the request names a manager the roster does not have.
var errUnknownManager = errors.New("no such manager")

// namedManager is the manager r names itself, "" when it names none.
func namedManager(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get(ManagerHeader)); id != "" {
		return id
	}
	return strings.TrimSpace(r.URL.Query().Get(ManagerQuery))
}

// requestManager is the manager a request that names no thing goes to: the one it names (errUnknownManager when the
// roster has no such manager), else the default.
func (h *Hub) requestManager(r *http.Request) (string, error) {
	id := namedManager(r)
	if id == "" {
		return h.defaultOwner(r.Context()), nil
	}
	if !h.knownManager(r.Context(), id) {
		return "", fmt.Errorf("%w: %q", errUnknownManager, clip(id, 64))
	}
	return id, nil
}

// knownManager: id is a manager of the roster (connected or not). Without a database, the ones connected and the
// assistant the hub starts.
func (h *Hub) knownManager(ctx context.Context, id string) bool {
	if id == assistantwire.AssistantID || h.busConnOf(id) != nil {
		return true
	}
	s := h.managerStore()
	if s == nil {
		return false
	}
	b, err := s.BusIdentityByID(ctx, id)
	return err == nil && b != nil && b.Role == bus.RoleManager
}

// ownerCache keeps the default manager for a moment: it is asked on every forwarded request.
type ownerCache struct {
	mu    sync.Mutex
	def   string
	until time.Time
}

const defaultOwnerTTL = 2 * time.Second

// defaultOwner is defaultManager, kept for a couple of seconds (managersChanged drops it).
func (h *Hub) defaultOwner(ctx context.Context) string {
	c := &h.owners
	c.mu.Lock()
	if c.def != "" && time.Now().Before(c.until) {
		def := c.def
		c.mu.Unlock()
		return def
	}
	c.mu.Unlock()
	def := h.defaultManager(ctx)
	c.mu.Lock()
	c.def, c.until = def, time.Now().Add(defaultOwnerTTL)
	c.mu.Unlock()
	return def
}

// ownersChanged forgets the kept default (the roster or the default changed).
func (h *Hub) ownersChanged() {
	c := &h.owners
	c.mu.Lock()
	c.def = ""
	c.mu.Unlock()
}

// machineOwner is the manager machine mid belongs to: the one that reported it, else the one that carries
// hub.json's machines.
func (h *Hub) machineOwner(mid string) string {
	if m := h.localMachine(mid); m != nil {
		if id := m.ownerID(); id != "" {
			return id
		}
	}
	return devicesOwner
}

// sessionOwner is the manager session sid on machine mid belongs to: the manager whose own conversation it is, else
// its machine's.
func (h *Hub) sessionOwner(mid, sid string) string {
	if id := h.conversationOwner(sid); id != "" {
		return id
	}
	return h.machineOwner(mid)
}

// conversationOwner is the manager whose snapshot lists conversation sid; "" for none.
func (h *Hub) conversationOwner(sid string) string {
	a := &h.assistant
	a.mu.RLock()
	defer a.mu.RUnlock()
	if _, ok := a.sessions[sid]; ok && sid != "" {
		return cmp.Or(a.from, assistantwire.AssistantID)
	}
	return ""
}

// interactionOwner is the manager that reported pending interaction id; "" when it is not pending.
func (h *Hub) interactionOwner(id string) string {
	if ix, ok := h.pendingInteraction(id); ok {
		return ix.Manager
	}
	return ""
}

// ---- forwarding by owner ----

// ownerOf resolves the manager a request goes to; an error is errUnknownManager.
type ownerOf func(r *http.Request) (string, error)

// byRequest: the manager the request names, else the default.
func (h *Hub) byRequest(r *http.Request) (string, error) { return h.requestManager(r) }

// byMachine: the request's machine ({mid}) decides; a request that also names a manager still goes to the
// machine's (the machine is the thing it is about).
func (h *Hub) byMachine(r *http.Request) (string, error) {
	mid := r.PathValue("mid")
	if mid == "" {
		mid = r.PathValue("id")
	}
	return h.machineOwner(mid), nil
}

// bySession: the request's session ({mid}/{sid}).
func (h *Hub) bySession(r *http.Request) (string, error) {
	return h.sessionOwner(r.PathValue("mid"), r.PathValue("sid")), nil
}

// byInteraction: the pending interaction ({id}) decides; one that is no longer pending goes by the request.
func (h *Hub) byInteraction(r *http.Request) (string, error) {
	if id := h.interactionOwner(r.PathValue("id")); id != "" {
		return id, nil
	}
	return h.requestManager(r)
}

// byConversation: the manager whose conversation {id} is; one no snapshot lists goes by the request.
func (h *Hub) byConversation(r *http.Request) (string, error) {
	if named := namedManager(r); named == "" {
		if id := h.conversationOwner(r.PathValue("id")); id != "" {
			return id, nil
		}
	}
	return h.requestManager(r)
}

// owner resolves r's manager with by, answering 404 no_manager itself; false once it answered.
func (h *Hub) owner(w http.ResponseWriter, r *http.Request, by ownerOf) (string, bool) {
	id, err := by(r)
	if err != nil {
		writeErrCode(w, http.StatusNotFound, "no_manager", err)
		return "", false
	}
	return id, true
}

// forwardOwned is forwardHTTP to the manager by resolves.
func (h *Hub) forwardOwned(w http.ResponseWriter, r *http.Request, by ownerOf) {
	if id, ok := h.owner(w, r, by); ok {
		h.forwardHTTP(w, r, id)
	}
}

// ---- merged lists ----

// mergedLists are the list routes whose answer the hub merges over every manager (route → the array that is the
// list); each item gets "manager". The rest of the answer is the default manager's.
var mergedLists = map[string]string{
	"GET /api/v1/inbox":      "items",
	"GET /api/v1/todos":      "todos",
	"GET /api/v1/extensions": "extensions",
	"GET /api/v1/plugins":    "plugins",
	"GET /api/v1/connectors": "connectors",
}

// listAnswer is one manager's answer to a list request.
type listAnswer struct {
	id     string
	status int
	header http.Header
	body   []byte
}

func (a listAnswer) ok() bool { return a.status >= 200 && a.status < 300 }

// bufferedWriter keeps what a handler writes (a manager's answer to a list request).
type bufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedWriter() *bufferedWriter { return &bufferedWriter{header: http.Header{}} }

func (b *bufferedWriter) Header() http.Header { return b.header }
func (b *bufferedWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
func (b *bufferedWriter) Flush() {} // a relayed answer flushes as it streams; this one is written whole

func (b *bufferedWriter) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

// listManagers are the managers a merged list asks: the default first, then the others connected now, by id.
func (h *Hub) listManagers(ctx context.Context) []string {
	def := h.defaultOwner(ctx)
	out := []string{def}
	var rest []string
	for _, p := range h.busRoster() {
		if p.Role == bus.RoleManager && p.ID != def {
			rest = append(rest, p.ID)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// mergeList answers a list request: a request naming a manager gets that manager's answer, else every manager is
// asked (ask, one after the other: the managers are few) and the lists under key merged, the default's first. Every
// item is stamped with its manager. A manager that does not answer is left out; when none does, the default's
// answer is given as it is.
func (h *Hub) mergeList(w http.ResponseWriter, r *http.Request, key string, ask func(w http.ResponseWriter, r *http.Request, id string)) {
	ids := []string{}
	if named := namedManager(r); named != "" {
		id, ok := h.owner(w, r, h.byRequest)
		if !ok {
			return
		}
		ids = append(ids, id)
	} else {
		ids = h.listManagers(r.Context())
	}
	body, err := readBody(w, r)
	if err != nil {
		return
	}
	var answers []listAnswer
	for _, id := range ids {
		bw := newBufferedWriter()
		ask(bw, withBody(r, body), id)
		answers = append(answers, listAnswer{id: id, status: cmp.Or(bw.status, http.StatusOK), header: bw.header, body: bw.body.Bytes()})
	}
	writeMerged(w, key, answers)
}

// writeMerged writes answers merged (see mergeList).
func writeMerged(w http.ResponseWriter, key string, answers []listAnswer) {
	var base *listAnswer
	var items []json.RawMessage
	var top map[string]json.RawMessage
	for i := range answers {
		a := &answers[i]
		if !a.ok() {
			continue
		}
		var t map[string]json.RawMessage
		if json.Unmarshal(a.body, &t) != nil {
			continue
		}
		raw, has := t[key]
		var list []json.RawMessage
		if has && json.Unmarshal(raw, &list) != nil {
			continue
		}
		for _, it := range list {
			items = append(items, stampManager(it, a.id))
		}
		if base == nil {
			base, top = a, t
		}
	}
	if base == nil { // nobody answered: the default's answer as it is
		a := answers[0]
		copyHeader(w.Header(), a.header)
		w.WriteHeader(a.status)
		_, _ = w.Write(a.body)
		return
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	list, _ := json.Marshal(items)
	top[key] = list
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(top)
	copyHeader(w.Header(), base.header)
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(base.status)
	_, _ = w.Write(buf.Bytes())
}

func copyHeader(dst, src http.Header) {
	for k, v := range src {
		dst[k] = v
	}
}

// stampManager is item (a JSON object) with "manager": id; anything else as it is.
func stampManager(item json.RawMessage, id string) json.RawMessage {
	t := bytes.TrimSpace(item)
	if len(t) < 2 || t[0] != '{' {
		return item
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(t, &fields) != nil {
		return item
	}
	idJSON, _ := json.Marshal(id)
	if _, has := fields[ManagerQuery]; has {
		fields[ManagerQuery] = idJSON
		out, _ := json.Marshal(fields)
		return out
	}
	var b bytes.Buffer
	b.WriteString(`{"manager":`)
	b.Write(idJSON)
	if len(fields) > 0 {
		b.WriteByte(',')
		b.Write(bytes.TrimSpace(t[1:]))
	} else {
		b.WriteByte('}')
	}
	return b.Bytes()
}

// readBody reads r's body (a list request has none, or a small one) so every manager can be asked with it; it
// answered when it fails.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	var b bytes.Buffer
	if _, err := b.ReadFrom(http.MaxBytesReader(w, r.Body, busHTTPLimit)); err != nil {
		writeBodyErr(w, err, err)
		return nil, err
	}
	return b.Bytes(), nil
}

// withBody is r with body to read again.
func withBody(r *http.Request, body []byte) *http.Request {
	r2 := r.Clone(r.Context())
	if body == nil {
		r2.Body = http.NoBody
	} else {
		r2.Body = readCloser{bytes.NewReader(body)}
	}
	return r2
}

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }

// managerKey carries a resolved manager in a request's context (a handler wrapped around the forwarding, such as a
// reply's receipt, resolves it first).
type managerKey struct{}

func withManager(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), managerKey{}, id))
}

// managerOfContext is the manager withManager put on r; the assistant the hub starts when none.
func managerOfContext(r *http.Request) string {
	if id, ok := r.Context().Value(managerKey{}).(string); ok && id != "" {
		return id
	}
	return assistantwire.AssistantID
}

// notifyManagers sends every connected manager a one-way event (what the hub saw that is no one manager's: the apps
// it has, a mute, a source with something new); errBusOffline when none is connected.
func (h *Hub) notifyManagers(event string, params any) error {
	sent := false
	for _, p := range h.busRoster() {
		if p.Role == bus.RoleManager && h.notifyBus(p.ID, event, params) == nil {
			sent = true
		}
	}
	if !sent {
		return errBusOffline
	}
	return nil
}

// managerID is the manager c is; the assistant the hub starts for none (a test that plays the bus).
func (c *busConn) managerID() string {
	if c == nil || c.ident.ID == "" {
		return assistantwire.AssistantID
	}
	return c.ident.ID
}
