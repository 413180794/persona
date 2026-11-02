package agents

import (
	"sync"
	"time"
)

// cache keeps each source's session list (for canSend checks and change detection) and the latest message page
// of recently opened sessions; agent-bridge.updated and the poll drop exactly the sessions that changed.
type cache struct {
	mu       sync.Mutex
	ttl      time.Duration
	sessions map[string]sessionsEntry // source → list
	latest   map[string]*latestEntry  // public id → newest page
}

type sessionsEntry struct {
	list []Session
	at   time.Time
}

type latestEntry struct {
	page  *MessagePage
	limit int
	at    time.Time
}

func newCache(ttl time.Duration) *cache {
	return &cache{ttl: ttl, sessions: map[string]sessionsEntry{}, latest: map[string]*latestEntry{}}
}

func (c *cache) getSessions(source string) ([]Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.sessions[source]
	return e.list, ok && time.Since(e.at) < c.ttl
}

func (c *cache) putSessions(source string, list []Session) {
	c.mu.Lock()
	c.sessions[source] = sessionsEntry{list: list, at: time.Now()}
	c.mu.Unlock()
}

func (c *cache) getLatest(id string, limit int) (*MessagePage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.latest[id]
	if e == nil || e.limit != limit || time.Since(e.at) >= c.ttl {
		return nil, false
	}
	return e.page, true
}

func (c *cache) putLatest(id string, limit int, p *MessagePage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.latest) > 200 { // a bound, not an LRU: dropping all is fine for a cache this cheap to refill
		c.latest = map[string]*latestEntry{}
	}
	c.latest[id] = &latestEntry{page: p, limit: limit, at: time.Now()}
}

// invalidate drops the session list of source and the cached pages of the given ids.
func (c *cache) invalidate(source string, ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, source)
	for _, id := range ids {
		delete(c.latest, id)
	}
}
