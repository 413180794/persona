package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ContributionKey names one contribution: "<point>/<id>" (Record.DisabledContributions).
func ContributionKey(point, id string) string { return point + "/" + id }

// SwitchedElsewhere says whether a contribution's switch is not the extension's own (docs/extensions.md「贡献的开关」):
// a connector's is the connector's switch (设置 › 连接器, the inbox and the archive keep using it), and a channel bound
// to a connector goes with that connector.
func (m *Manifest) SwitchedElsewhere(point, id string) bool {
	if point == "connectors" {
		return true
	}
	if point == "channel" {
		for _, c := range m.Channels() {
			if c.ID == id && c.Connector != "" {
				return true
			}
		}
	}
	return false
}

// Without is the manifest with the contributions in disabled ("<point>/<id>") taken out: what takes effect. Keys it
// does not have and the ones switched elsewhere (SwitchedElsewhere) are ignored. The manifest itself when nothing is
// taken out.
func (m *Manifest) Without(disabled []string) *Manifest {
	if m == nil || len(disabled) == 0 {
		return m
	}
	var out *Manifest
	for _, name := range sortedKeys(m.Contributes) {
		p, known := Points[name]
		if !known {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(m.Contributes[name], &items) != nil {
			continue
		}
		kept := items[:0:0]
		for _, raw := range items {
			id, err := p.Item(raw)
			if err == nil && slices.Contains(disabled, ContributionKey(name, id)) && !m.SwitchedElsewhere(name, id) {
				continue
			}
			kept = append(kept, raw)
		}
		if len(kept) == len(items) {
			continue
		}
		if out == nil {
			c := *m
			c.Contributes = maps.Clone(m.Contributes)
			out = &c
		}
		if len(kept) == 0 {
			delete(out.Contributes, name)
		} else {
			out.Contributes[name], _ = json.Marshal(kept)
		}
	}
	if out == nil {
		return m
	}
	return out
}

// ContributionTitle is what a contribution is called for people: its name or title, else its id.
func ContributionTitle(m *Manifest, point, id string) string {
	var items []json.RawMessage
	_ = json.Unmarshal(m.Contributes[point], &items)
	p, ok := Points[point]
	if !ok {
		return id
	}
	for _, raw := range items {
		if got, err := p.Item(raw); err != nil || got != id {
			continue
		}
		var named struct {
			Name  string `json:"name"`
			Title string `json:"title"`
		}
		_ = json.Unmarshal(raw, &named)
		if t := strings.TrimSpace(named.Title); t != "" {
			return t
		}
		if n := strings.TrimSpace(named.Name); n != "" {
			return n
		}
	}
	return id
}

// ErrSwitchedElsewhere: the contribution's switch is not the extension's (a connector's).
var ErrSwitchedElsewhere = errors.New("这一项跟着连接器的开关")

// SetContribution switches one contribution of extension id on or off; the extension restarts with it taken out
// (Change kind contributions). hub.json's are changed there only.
func (r *Registry) SetContribution(ctx context.Context, id, point, cid string, on bool, actor string) error {
	p, err := r.editable(id)
	if err != nil {
		return err
	}
	if !slices.Contains(p.Full.Items(point), cid) {
		return fmt.Errorf("扩展 %s 没有贡献 %s/%s", id, point, cid)
	}
	if p.Full.SwitchedElsewhere(point, cid) {
		return ErrSwitchedElsewhere
	}
	key := ContributionKey(point, cid)
	r.mu.Lock()
	row := p.Record
	off := slices.DeleteFunc(slices.Clone(row.DisabledContributions), func(k string) bool { return k == key })
	if !on {
		off = append(off, key)
		slices.Sort(off)
	}
	same := slices.Equal(off, row.DisabledContributions)
	row.DisabledContributions = off
	r.mu.Unlock()
	if same {
		return nil
	}
	if err := r.put(ctx, row); err != nil {
		return err
	}
	r.mu.Lock()
	p.Record.DisabledContributions = off
	p.Manifest = p.Full.Without(off)
	r.mu.Unlock()
	r.audit(ctx, id, "contribution", actor, map[string]any{"contribution": key, "enabled": on})
	r.changed(Change{ID: id, Kind: "contributions"})
	return nil
}
