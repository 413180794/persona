package host

import (
	"slices"
	"strings"
	"sync"
)

// PaneRef is a pane of some host: the host's kind and the host's own id for it.
//
// Stored and sent (the hub's database, sessions, hook payloads) as String: "<kind>:<id>", except herdr panes, which
// stay bare as they always were ("w1:p2") — a string with no known host prefix is a herdr pane.
type PaneRef struct {
	Kind string
	ID   string
}

// String is the stored form: bare for herdr, "<kind>:<id>" for any other host.
func (p PaneRef) String() string {
	if p.Kind == Herdr || p.Kind == "" {
		return p.ID
	}
	return p.Kind + ":" + p.ID
}

// Registry holds the hosts a node drives, by kind, in the order added. Set replaces them all at once (the node
// switching engines); the slices it hands out are never changed afterwards.
type Registry struct {
	mu    sync.RWMutex
	hosts []Host
}

// NewRegistry holds hosts; the first one of a kind wins.
func NewRegistry(hosts ...Host) *Registry {
	r := &Registry{}
	r.Set(hosts...)
	return r
}

// Set replaces the hosts; the first one of a kind wins.
func (r *Registry) Set(hosts ...Host) {
	var out []Host
	for _, h := range hosts {
		if h != nil && !slices.ContainsFunc(out, func(o Host) bool { return o.Kind() == h.Kind() }) {
			out = append(out, h)
		}
	}
	r.mu.Lock()
	r.hosts = out
	r.mu.Unlock()
}

// All are the hosts in the order added.
func (r *Registry) All() []Host {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.hosts
}

// Get is the host of a kind, nil when none is registered.
func (r *Registry) Get(kind string) Host {
	for _, h := range r.All() {
		if h.Kind() == kind {
			return h
		}
	}
	return nil
}

// Ref parses a stored pane (ParsePane); it does not depend on which hosts are registered.
func (r *Registry) Ref(pane string) PaneRef { return ParsePane(pane) }

// ParsePane parses a stored pane: "<kind>:<id>" for a known host kind other than herdr (Kinds), anything else is a
// herdr pane — herdr's own ids hold a colon too ("w79:p2"), and so does an unprefixed "<uuid>:<uuid>".
func ParsePane(pane string) PaneRef {
	if kind, id, ok := strings.Cut(pane, ":"); ok && kind != Herdr && slices.Contains(Kinds, kind) {
		return PaneRef{Kind: kind, ID: id}
	}
	return PaneRef{Kind: Herdr, ID: pane}
}

// Pane is the host a stored pane belongs to (nil when that host is not registered on this machine) and its ref.
func (r *Registry) Pane(pane string) (Host, PaneRef) {
	ref := r.Ref(pane)
	return r.Get(ref.Kind), ref
}
