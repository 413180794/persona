package agents

import (
	"context"
	"net/http"
	"sync"
	"time"
)

const prefsKey = "agents.prefs"

// View is how the apps lay out the personal-agents list. Empty fields mean the defaults (group by date, sort by
// last update, show all, every provider).
type View struct {
	Group     string   `json:"group,omitempty"` // date | provider | none
	Sort      string   `json:"sort,omitempty"`  // updated | unread | name
	Show      string   `json:"show,omitempty"`  // all | unread | sendable
	Providers []string `json:"providers,omitempty"`
}

// Section is a user-named group; Assign maps a session id to a section id.
type Section struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Prefs is shared by every app of the user: pinned sessions (in pin order), the list view, and custom sections.
type Prefs struct {
	Pins      []string          `json:"pins"`
	View      View              `json:"view"`
	Sections  []Section         `json:"sections"`
	Assign    map[string]string `json:"assign"`
	UpdatedAt time.Time         `json:"updatedAt,omitzero"`
}

type prefsStore struct {
	mu  sync.Mutex
	kv  KV
	mem *Prefs
}

func (p *Prefs) normalize() {
	if p.Pins == nil {
		p.Pins = []string{}
	}
	if p.Sections == nil {
		p.Sections = []Section{}
	}
	if p.Assign == nil {
		p.Assign = map[string]string{}
	}
	seen := map[string]bool{}
	pins := p.Pins[:0]
	for _, id := range p.Pins {
		if id != "" && !seen[id] {
			seen[id] = true
			pins = append(pins, id)
		}
	}
	p.Pins = pins
	ids := map[string]bool{}
	for _, s := range p.Sections {
		ids[s.ID] = true
	}
	for k, v := range p.Assign {
		if !ids[v] {
			delete(p.Assign, k)
		}
	}
}

func (s *prefsStore) get(ctx context.Context) (Prefs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p Prefs
	if s.kv != nil {
		if _, err := s.kv.Setting(ctx, prefsKey, &p); err != nil {
			return p, err
		}
	} else if s.mem != nil {
		p = *s.mem
	}
	p.normalize()
	return p, nil
}

func (s *prefsStore) put(ctx context.Context, p Prefs) (Prefs, error) {
	if len(p.Pins) > 1000 || len(p.Sections) > 100 || len(p.Assign) > 5000 {
		return p, fail(http.StatusBadRequest, "agents: prefs too large")
	}
	for _, sec := range p.Sections {
		if sec.ID == "" || sec.Name == "" {
			return p, fail(http.StatusBadRequest, "agents: section needs id and name")
		}
	}
	p.normalize()
	p.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kv != nil {
		if err := s.kv.PutSetting(ctx, prefsKey, p); err != nil {
			return p, err
		}
	} else {
		c := p
		s.mem = &c
	}
	return p, nil
}

// Prefs returns the shared list preferences (defaults when never saved).
func (s *Service) Prefs(ctx context.Context) (Prefs, error) { return s.prefs.get(ctx) }

// PutPrefs replaces the shared list preferences.
func (s *Service) PutPrefs(ctx context.Context, p Prefs) (Prefs, error) { return s.prefs.put(ctx, p) }
