package hub

import (
	"github.com/nwverse/persona/server/internal/mdns"
	"sync"
)

// A goodbye removes a service record, not an identity still advertised by the
// other service. Both browse loops call this adapter concurrently.
type compatibleDiscovery struct {
	mu      sync.Mutex
	entries map[string]map[string]mdns.Entry
	publish func(mdns.Entry)
}

func (d *compatibleDiscovery) found(service string) func(mdns.Entry) {
	return func(e mdns.Entry) {
		id := e.TXT["hub_id"]
		if id == "" {
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.entries == nil {
			d.entries = make(map[string]map[string]mdns.Entry)
		}
		records := d.entries[id]
		if records == nil {
			if e.TTL == 0 || len(d.entries) >= 64 {
				return
			}
			records = make(map[string]mdns.Entry)
			d.entries[id] = records
		}
		if e.TTL == 0 {
			delete(records, service)
		} else {
			records[service] = e
		}
		if len(records) == 0 {
			delete(d.entries, id)
			d.publish(e)
			return
		}
		latest := mdns.Entry{}
		for _, entry := range records {
			if latest.TTL == 0 || entry.Seen.After(latest.Seen) {
				latest = entry
			}
		}
		d.publish(latest)
	}
}
