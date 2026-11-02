package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

var validID = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Ask asks the user on the phone to allow a write (the hub's approval card) and says whether they did.
type Ask func(ctx context.Context, summary string, input json.RawMessage) bool

// Registry holds the hub's connectors, in the order added, and the write gate's rules.
type Registry struct {
	mu    sync.RWMutex
	list  []Connector
	rules []Rule
}

// NewRegistry holds no connector yet; rules are the write gate's (hub.json connector_gate, ValidateRules).
func NewRegistry(rules []Rule) *Registry { return &Registry{rules: rules} }

// Add registers a connector; its ID must be new and look like one.
func (r *Registry) Add(c Connector) error {
	id := c.ID()
	if !validID.MatchString(id) {
		return fmt.Errorf("connector id %q: lowercase letters and digits only", id)
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

// StatusOf is a connector's status; one without Accounts is always ok.
func StatusOf(ctx context.Context, c Connector) Status {
	if a, ok := c.(Accounts); ok {
		return a.Status(ctx)
	}
	return Status{State: StateOK}
}

// Tools are the read tools and the actions of every connector that is ok, in the order added.
func (r *Registry) Tools(ctx context.Context) []proto.Tool {
	var out []proto.Tool
	for _, c := range r.All() {
		if StatusOf(ctx, c).State != StateOK {
			continue
		}
		if t, ok := c.(ReadTools); ok {
			out = append(out, owned(c, t.Tools())...)
		}
		if a, ok := c.(Actions); ok {
			out = append(out, owned(c, a.Actions())...)
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
	if t, ok := c.(ReadTools); ok && has(t.Tools(), name) {
		return result(t.Call(ctx, name, args))
	}
	a, ok := c.(Actions)
	if !ok || !has(a.Actions(), name) {
		return proto.TextResult("unknown tool "+name, true)
	}
	r.mu.RLock()
	rules := r.rules
	r.mu.RUnlock()
	switch Decide(rules, c.ID(), name, args) {
	case Deny:
		return proto.TextResult("The hub's rules (connector_gate) do not allow "+name+".", true)
	case Confirm:
		act, summary := a.Describe(ctx, name, args)
		if act.Visible != "" {
			summary += "\n\n谁会看到：" + act.Visible
		}
		if ask == nil || !ask(ctx, summary, args) {
			return proto.TextResult("The user declined "+name+".", true)
		}
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
	LastSync     *time.Time `json:"last_sync,omitempty"`
	LastEvent    *time.Time `json:"last_event,omitempty"`
	Capabilities []string   `json:"capabilities"`
	Hosts        []string   `json:"hosts,omitempty"`
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
		info := Info{ID: c.ID(), Name: c.Name(), Status: st.State, Account: st.Account, Via: st.Via, Error: st.Error,
			LastSync: when(st.LastSync), LastEvent: when(st.LastEvent), Capabilities: Capabilities(c)}
		if h, ok := c.(Hosts); ok {
			info.Hosts = h.Hosts()
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

// Capabilities names the optional interfaces a connector implements.
func Capabilities(c Connector) []string {
	out := []string{}
	add := func(name string, ok bool) {
		if ok {
			out = append(out, name)
		}
	}
	_, status := c.(Accounts)
	_, tools := c.(ReadTools)
	_, actions := c.(Actions)
	_, events := c.(Events)
	_, hosts := c.(Hosts)
	_, inbox := c.(Inbox)
	_, archive := c.(Archive)
	add("status", status)
	add("tools", tools)
	add("actions", actions)
	add("events", events)
	add("hosts", hosts)
	add("inbox", inbox)
	add("archive", archive)
	return out
}
