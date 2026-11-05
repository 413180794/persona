package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
)

// The extension model in the hub (docs/extensions.md「依赖、导出和合集」, #1854): what installs together (a preview's
// bundle: missing dependencies, a pack's members), what a switch-off or removal takes with it (dependents), and
// telling an exporter its contributions changed.

// bundleDepth is how deep dependencies of dependencies are followed.
const bundleDepth = 4

// findInMarkets is extension id in a market: the one at spec (an address as registered) when given, else prefer's,
// else the first registered market (by id) that has it.
func (h *Hub) findInMarkets(id, spec, prefer string) (extension.MarketRecord, *extension.MarketEntry, error) {
	ms := h.marketsState()
	ms.mu.Lock()
	var order []string
	if spec != "" {
		if parsed, err := extension.ParseMarket(spec); err == nil {
			if s := ms.withSpec(parsed.String()); s != nil {
				order = append(order, s.rec.ID)
			}
		}
		if len(order) == 0 {
			ms.mu.Unlock()
			return extension.MarketRecord{}, nil, fmt.Errorf("没有登记市场 %s", spec)
		}
	} else {
		if prefer != "" && ms.byID[prefer] != nil {
			order = append(order, prefer)
		}
		var rest []string
		for mid := range ms.byID {
			if mid != prefer {
				rest = append(rest, mid)
			}
		}
		sort.Strings(rest)
		order = append(order, rest...)
	}
	ms.mu.Unlock()
	var last error = errors.New("登记的市场里都没有")
	for _, mid := range order {
		rec, e, err := h.marketEntry(mid, id)
		if err == nil {
			return rec, e, nil
		}
		last = err
	}
	return extension.MarketRecord{}, nil, last
}

// resolveBundle fills pv's Bundle with the previews of what installs with it from the markets: its dependencies that
// are missing or too old, a pack's members not installed (or too old), and theirs in turn; Unresolved says what no
// market has. market is the market pv comes from ("" for one from elsewhere).
func (h *Hub) resolveBundle(ctx context.Context, pv *extension.Preview, market string) {
	if h.extensions == nil || h.marketStore() == nil {
		return
	}
	reg := h.extensions.reg
	seen := map[string]bool{pv.Manifest.ID: true}
	type want struct{ id, spec, version string }
	var walk func(m *extension.Manifest, market string, depth int)
	walk = func(m *extension.Manifest, market string, depth int) {
		var wants []want
		for _, d := range reg.Dependencies(m) {
			if d.State == extension.DepMissing || d.State == extension.DepOld {
				wants = append(wants, want{d.ID, "", d.Version})
			}
		}
		for _, pm := range m.ExtensionPack {
			cur := reg.Get(pm.ID)
			if cur == nil || cur.Record.Source == extension.SourceInstalled && !(extension.Dependency{ID: pm.ID, Version: pm.Version}).Allows(cur.Record.Version) {
				wants = append(wants, want{pm.ID, pm.Market, pm.Version})
			}
		}
		for _, w := range wants {
			if seen[w.id] {
				continue
			}
			seen[w.id] = true
			if depth > bundleDepth {
				pv.Unresolved = append(pv.Unresolved, w.id+"（依赖太深）")
				continue
			}
			rec, e, err := h.findInMarkets(w.id, w.spec, market)
			if err != nil {
				pv.Unresolved = append(pv.Unresolved, w.id+"（"+err.Error()+"）")
				continue
			}
			if !(extension.Dependency{ID: w.id, Version: w.version}).Allows(e.Parsed.Version) {
				pv.Unresolved = append(pv.Unresolved, fmt.Sprintf("%s（要 %s，市场里是 %s）", w.id, w.version, e.Parsed.Version))
				continue
			}
			mk, err := h.market(rec)
			if err != nil {
				pv.Unresolved = append(pv.Unresolved, w.id+"（"+err.Error()+"）")
				continue
			}
			upgrade := ""
			if cur := reg.Get(w.id); cur != nil && cur.Record.Source == extension.SourceInstalled {
				upgrade = w.id
			}
			sub, err := reg.PreviewMarket(ctx, mk, rec.ID, e, upgrade)
			if err != nil {
				pv.Unresolved = append(pv.Unresolved, w.id+"（"+err.Error()+"）")
				continue
			}
			walk(sub.Manifest, rec.ID, depth+1) // what it needs installs before it
			pv.Bundle = append(pv.Bundle, sub)
		}
	}
	walk(pv.Manifest, market, 1)
}

// bundleRef is one preview of a bundle as the install request sends it back.
type bundleRef struct {
	PreviewID string `json:"preview_id"`
	Digest    string `json:"permissions_digest"`
}

// installBundle installs (or upgrades) what main's preview bundles, in order, after checking the request names every
// one with the digest the user saw. Nothing is installed when one does not match.
func (h *Hub) installBundle(ctx context.Context, main *extension.Preview, digest string, refs []bundleRef, actor string) error {
	if main == nil || main.Digest != digest {
		return nil // expired, used or not what the user saw: Install says so, before anything installs
	}
	if len(main.Unresolved) > 0 {
		return errors.New("还缺这些扩展，市场里找不到：" + strings.Join(main.Unresolved, "、"))
	}
	bundled := func(id string) bool {
		return slices.ContainsFunc(main.Bundle, func(b *extension.Preview) bool { return b.Manifest.ID == id })
	}
	for _, d := range h.extensions.reg.Dependencies(main.Manifest) {
		if d.State == extension.DepMissing && !bundled(d.ID) {
			return errors.New("先装它要的扩展 " + d.ID)
		}
	}
	if len(refs) != len(main.Bundle) {
		return fmt.Errorf("要一起装 %d 个扩展：确认它们后一起发过来（bundle）", len(main.Bundle))
	}
	for i, b := range main.Bundle {
		if refs[i].PreviewID != b.ID || refs[i].Digest != b.Digest {
			return fmt.Errorf("一起装的 %s 和预览里的对不上：重新预览", b.Manifest.ID)
		}
	}
	reg := h.extensions.reg
	for _, b := range main.Bundle {
		var err error
		if b.Upgrade {
			_, err = reg.Upgrade(ctx, b.Manifest.ID, b.ID, b.Digest, actor)
		} else {
			_, err = reg.Install(ctx, b.ID, b.Digest, actor)
		}
		if err != nil {
			return fmt.Errorf("一起装的 %s 没装上：%w", b.Manifest.ID, err)
		}
	}
	return nil
}

// DependentView is an extension that needs another, for the refusal of switching that one off.
type DependentView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// enabledDependents follows the complete dependent graph, deepest first.
func (ps *extensions) enabledDependents(id string) []DependentView {
	return ps.dependentsOf([]string{id})
}

func (ps *extensions) dependentsOf(ids []string) []DependentView {
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	var out []DependentView
	var visit func(string)
	visit = func(id string) {
		for _, p := range ps.reg.Dependents(id) {
			if seen[p.Record.ID] {
				continue
			}
			seen[p.Record.ID] = true
			visit(p.Record.ID)
			if p.Record.Enabled {
				out = append(out, DependentView{ID: p.Record.ID, Name: p.Full.Name})
			}
		}
	}
	for _, id := range ids {
		visit(id)
	}
	return out
}

// takeDependents asks before switching off dependents; no write precedes the full read-only check.
func (h *Hub) takeDependents(w http.ResponseWriter, r *http.Request, id string, cascade bool) bool {
	return h.takeDependentsOf(w, r, []string{id}, cascade)
}

func (h *Hub) takeDependentsOf(w http.ResponseWriter, r *http.Request, ids []string, cascade bool) bool {
	deps := h.extensions.dependentsOf(ids)
	if len(deps) == 0 {
		return true
	}
	if !cascade {
		var names []string
		for _, d := range deps {
			names = append(names, d.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "has_dependents", "dependents": deps,
			"error": "这些扩展要用它：" + strings.Join(names, "、") + "。连它们一起停用？"})
		return false
	}
	for _, d := range deps {
		if p := h.extensions.reg.Get(d.ID); p != nil && p.ReadOnly() {
			writeErrCode(w, http.StatusConflict, "read_only", errors.New("扩展「"+d.Name+"」写在 hub.json 里，请先在那里停用"))
			return false
		}
	}
	for _, d := range deps {
		if err := h.extensions.reg.SetEnabled(r.Context(), d.ID, false, actorOr(r, "personad extension")); err != nil {
			writeErrCode(w, http.StatusConflict, "read_only", err)
			return false
		}
	}
	return true
}

// packMembers are the installed members of pack p, to remove with it.
func (ps *extensions) packMembers(p *extension.Extension) []string {
	var out []string
	for _, m := range p.Full.ExtensionPack {
		if cur := ps.reg.Get(m.ID); cur != nil && cur.Record.Source == extension.SourceInstalled {
			out = append(out, m.ID)
		}
	}
	return out
}

// modelChanged follows a change of one extension through the others: the ones depending on it start or wait
// (Missing), and the extensions it contributes to hear of their contributions (extension.contributions_changed).
func (ps *extensions) modelChanged(ctx context.Context, c extension.Change) {
	for _, p := range ps.reg.Dependents(c.ID) {
		ps.apply(ctx, p.Record.ID)
	}
	var to []string
	if now := ps.reg.Get(c.ID); now != nil {
		to = now.Full.ContributesTo()
	}
	if c.Old != nil {
		for _, id := range c.Old.Full.ContributesTo() {
			if !slices.Contains(to, id) {
				to = append(to, id)
			}
		}
	}
	for _, id := range to {
		if hp := ps.get(id); hp != nil && hp.proc != nil {
			_ = hp.proc.Notify("extension.contributions_changed", map[string]any{"contributions_to_me": ps.reg.ContributionsTo(id)})
		}
	}
}
