package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

var validID = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Ask asks the user on the phone to allow a write (the hub's approval card) and says whether they did.
type Ask func(ctx context.Context, summary string, input json.RawMessage) bool

// Registry holds the hub's connectors, in the order added, and the write gate's rules.
type Registry struct {
	mu    sync.RWMutex
	list  []Connector
	rules []Rule
	off   map[string]bool // switched off by the user (SetEnabled)
	// run is what Start was given, to start a connector switched on later; stop cancels a running one's Run / Watch.
	run  *starter
	stop map[string]context.CancelFunc
}

type starter struct {
	ctx     context.Context
	changed func(id string, scope Scope)
	logf    func(string, ...any)
}

// NewRegistry holds no connector yet; rules are the write gate's (hub.json connector_gate, ValidateRules).
func NewRegistry(rules []Rule) *Registry {
	return &Registry{rules: rules, off: map[string]bool{}, stop: map[string]context.CancelFunc{}}
}

// switchedOff are the connectors switched off, by instance: StatusOf answers StateDisabled for them, so every pipe
// that goes by the status (tools, the inbox, the archive) leaves them alone without knowing about the switch.
var switchedOff sync.Map

// SetEnabled switches a connector on or off. Off, it does not run (its Run and Watch are cancelled), offers no tools
// and brings nothing into the inbox or the knowledge base, and its status is StateDisabled; on, it starts again.
// Signing in and out (Login) still work while it is off. The hub keeps the choice (it calls this before Start).
func (r *Registry) SetEnabled(id string, on bool) error {
	c := r.Get(id)
	if c == nil {
		return fmt.Errorf("no connector %q", id)
	}
	if !reflect.TypeOf(c).Comparable() {
		return fmt.Errorf("connector %q cannot be switched off (not comparable)", id)
	}
	r.mu.Lock()
	var cancel context.CancelFunc
	if on {
		delete(r.off, id)
		switchedOff.Delete(c)
	} else {
		r.off[id] = true
		switchedOff.Store(c, true)
		cancel = r.stop[id]
		delete(r.stop, id)
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if on {
		r.startOne(c)
	}
	return nil
}

// Enabled says whether a connector is switched on (every one is, until SetEnabled says otherwise).
func (r *Registry) Enabled(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.off[id]
}

// Add registers a connector; its ID must be new and look like one, its manifest pass Check.
func (r *Registry) Add(c Connector) error {
	id := c.ID()
	if !validID.MatchString(id) {
		return fmt.Errorf("connector id %q: lowercase letters and digits only", id)
	}
	if err := Check(c); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.ContainsFunc(r.list, func(o Connector) bool { return o.ID() == id }) {
		return fmt.Errorf("connector %q is registered twice", id)
	}
	r.list = append(r.list, c)
	return nil
}

// All are the connectors in the order added.
func (r *Registry) All() []Connector {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.list)
}

// Get is the connector with id, nil when none is registered.
func (r *Registry) Get(id string) Connector {
	for _, c := range r.All() {
		if c.ID() == id {
			return c
		}
	}
	return nil
}

// Owner is the connector a tool name belongs to (its "<ID>_" prefix), nil when none.
func (r *Registry) Owner(tool string) Connector {
	id, _, ok := strings.Cut(tool, "_")
	if !ok {
		return nil
	}
	return r.Get(id)
}

// ToolNames are the names of every read tool and action a connector offers, whatever its status.
func ToolNames(c Connector) []string {
	var out []string
	for _, tl := range allTools(c) {
		out = append(out, tl.Name)
	}
	return out
}

// allTools are c's read tools, then its actions.
func allTools(c Connector) []proto.Tool {
	var out []proto.Tool
	if t, ok := As[ReadTools](c); ok {
		out = append(out, t.Tools()...)
	}
	if a, ok := As[Actions](c); ok {
		out = append(out, a.Actions()...)
	}
	return out
}

// Offers says whether a registered connector offers the tool name (its own: the "<ID>_" prefix and in its list).
// A connector named like the hub's own tools ("lark") does not take those over until it offers them itself.
func (r *Registry) Offers(name string) bool {
	c := r.Owner(name)
	return c != nil && slices.Contains(ToolNames(c), name)
}

// ForAgents says whether the tool name is a connector's that the coding agents get too (Manifest.AgentTools).
func (r *Registry) ForAgents(name string) bool {
	c := r.Owner(name)
	return c != nil && c.Manifest().ForAgents(name)
}

// StatusOf is a connector's status: StateDisabled when the user switched it off; one without a status is always ok.
func StatusOf(ctx context.Context, c Connector) Status {
	if c != nil && reflect.TypeOf(c).Comparable() {
		if _, off := switchedOff.Load(c); off {
			return Status{State: StateDisabled}
		}
	}
	// A proxy's status is always its own: it answers for a program while the program does not run.
	if a, ok := c.(Accounts); ok && (c.Manifest().Has(CapStatus) || c.Manifest().Proxy) {
		return a.Status(ctx)
	}
	return Status{State: StateOK}
}

// Tools are the read tools and the actions of every connector that is ok, in the order added.
func (r *Registry) Tools(ctx context.Context) []proto.Tool {
	return append(r.ToolsFor(ctx, true), r.ToolsFor(ctx, false)...)
}

// ToolsFor are the tools of the connectors that are ok that go to the agents too (agents), or only to the
// assistant (!agents), in the order added.
func (r *Registry) ToolsFor(ctx context.Context, agents bool) []proto.Tool {
	var out []proto.Tool
	for _, c := range r.All() {
		if StatusOf(ctx, c).State != StateOK {
			continue
		}
		m := c.Manifest()
		for _, t := range owned(c, allTools(c)) {
			if m.ForAgents(t.Name) == agents {
				out = append(out, t)
			}
		}
	}
	return out
}

// owned drops tools that do not carry the connector's prefix: they would be dispatched to someone else.
func owned(c Connector, tools []proto.Tool) []proto.Tool {
	return slices.DeleteFunc(slices.Clone(tools), func(t proto.Tool) bool { return !strings.HasPrefix(t.Name, c.ID()+"_") })
}

func has(tools []proto.Tool, name string) bool {
	return slices.ContainsFunc(tools, func(t proto.Tool) bool { return t.Name == name })
}

// Call runs the tool name: a read tool right away, an action only when the gate lets it through — Allow runs it,
// Deny refuses, Confirm runs it once ask says the user allowed it on the phone.
func (r *Registry) Call(ctx context.Context, name string, args json.RawMessage, ask Ask) proto.ToolResult {
	c := r.Owner(name)
	if c == nil {
		return proto.TextResult("unknown tool "+name, true)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if st := StatusOf(ctx, c); st.State != StateOK {
		why := st.Error
		if why == "" {
			why = st.State
		}
		return proto.TextResult(fmt.Sprintf("%s is not available: %s", c.Name(), why), true)
	}
	if t, ok := As[ReadTools](c); ok && has(t.Tools(), name) {
		return result(t.Call(ctx, name, args))
	}
	a, ok := As[Actions](c)
	if !ok || !has(a.Actions(), name) {
		return proto.TextResult("unknown tool "+name, true)
	}
	r.mu.RLock()
	rules := r.rules
	r.mu.RUnlock()
	if res := gate(ctx, rules, c, a, name, args, ask); res != nil {
		return *res
	}
	return result(a.Do(ctx, name, args))
}

func result(text string, err error) proto.ToolResult {
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(text, false)
}

// Info is one connector as GET /api/v1/connectors lists it.
type Info struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Account      string     `json:"account,omitempty"`
	Via          string     `json:"via,omitempty"`
	Error        string     `json:"error,omitempty"`
	Guide        []string   `json:"guide,omitempty"`
	LastSync     *time.Time `json:"last_sync,omitempty"`
	LastEvent    *time.Time `json:"last_event,omitempty"`
	Capabilities []string   `json:"capabilities"`
	Hosts        []string   `json:"hosts,omitempty"`
	// Enabled: switched on (SetEnabled); off, Status is "disabled".
	Enabled bool `json:"enabled"`
	// Options are its own settings (Options), as it gives them.
	Options json.RawMessage `json:"options,omitempty"`
}

// MachineOnline tells every connector that depends on a computer that machine id just connected.
func (r *Registry) MachineOnline(id string) {
	for _, c := range r.All() {
		if m, ok := c.(Machines); ok {
			m.MachineOnline(id)
		}
	}
}

// Infos lists every connector with its status and what it can do.
func (r *Registry) Infos(ctx context.Context) []Info {
	out := []Info{}
	for _, c := range r.All() {
		st := StatusOf(ctx, c)
		info := Info{ID: c.ID(), Name: c.Name(), Status: st.State, Account: st.Account, Via: st.Via, Error: st.Error, Guide: st.Guide,
			LastSync: when(st.LastSync), LastEvent: when(st.LastEvent), Capabilities: Capabilities(c), Enabled: r.Enabled(c.ID())}
		if hosts := c.Manifest().Hosts; len(hosts) > 0 {
			info.Hosts = hosts
		}
		if o, ok := As[Options](c); ok {
			info.Options = o.Options()
		}
		out = append(out, info)
	}
	return out
}

func when(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
