package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
)

// Plugin markets (docs/plugins.md「插件市场」): the hub keeps the markets the user registered and their indexes, lists
// their plugins as one catalog, and installs one from a market. A device with full rights may do all of this; other
// sources (a directory, a file, a URL, an upload) stay with the hub's machine and the admin UI.

// marketStale is how old an index may be before the catalog reads it again (in the background).
const marketStale = 6 * time.Hour

type markets struct {
	mu      sync.Mutex
	loaded  bool
	byID    map[string]*marketState
	reading map[string]bool
	// Client and GitHubAPI: tests point them at a fake.
	client    *http.Client
	githubAPI string
}

type marketState struct {
	rec   plugin.MarketRecord
	index *plugin.MarketIndex
}

// MarketView is a market as the apps list it.
type MarketView struct {
	ID          string     `json:"id"`
	Spec        string     `json:"spec"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Homepage    string     `json:"homepage,omitempty"`
	HasToken    bool       `json:"has_token"`
	Plugins     int        `json:"plugins"`
	Warnings    []string   `json:"warnings"`
	Error       string     `json:"error,omitempty"`
	FetchedAt   *time.Time `json:"fetched_at,omitempty"`
	AddedAt     time.Time  `json:"added_at"`
}

// CatalogItem is a market's plugin as the catalog shows it, with what this hub has of it.
type CatalogItem struct {
	Market      string                  `json:"market"`
	MarketName  string                  `json:"market_name"`
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Version     string                  `json:"version"`
	Description string                  `json:"description,omitempty"`
	Publisher   string                  `json:"publisher,omitempty"`
	Homepage    string                  `json:"homepage,omitempty"`
	Tags        []string                `json:"tags"`
	Contributes map[string][]string     `json:"contributes"`
	Permissions []plugin.PermissionLine `json:"permissions"`
	// Problem: why this hub cannot install it (no package for its platform, a newer hub needed); "" when it can.
	Problem string `json:"problem,omitempty"`
	// Installed is the version this hub has ("" none); InstalledSource where that one is from (builtin, hub.json,
	// installed); FromThisMarket whether it was installed from this market; Upgradable whether this one is newer.
	Installed       string `json:"installed,omitempty"`
	InstalledSource string `json:"installed_source,omitempty"`
	FromThisMarket  bool   `json:"from_this_market,omitempty"`
	Upgradable      bool   `json:"upgradable,omitempty"`
}

func (h *Hub) marketStore() plugin.MarketStore {
	s, _ := h.store.(plugin.MarketStore)
	return s
}

func (h *Hub) marketsState() *markets {
	if h.plugins == nil {
		return nil
	}
	return &h.plugins.markets
}

// load reads the markets from the store once.
func (h *Hub) marketsLoad(ctx context.Context) error {
	ms, st := h.marketsState(), h.marketStore()
	if ms == nil || st == nil {
		return errors.New("这个 hub 没有数据库：用不了插件市场")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.loaded {
		return nil
	}
	rows, err := st.PluginMarkets(ctx)
	if err != nil {
		return err
	}
	ms.byID = map[string]*marketState{}
	for _, r := range rows {
		s := &marketState{rec: r}
		if len(r.Index) > 0 {
			s.index, _, _ = plugin.ParseIndex(r.Index)
		}
		ms.byID[r.ID] = s
	}
	ms.loaded = true
	return nil
}

func (h *Hub) market(rec plugin.MarketRecord) (*plugin.Market, error) {
	spec, err := plugin.ParseMarket(rec.Spec)
	if err != nil {
		return nil, err
	}
	mk := &plugin.Market{Spec: spec, Client: h.plugins.markets.client, GitHubAPI: h.plugins.markets.githubAPI, Dir: h.marketsDir()}
	if rec.TokenSealed != "" {
		if mk.Token, err = (hubSealer{h}).Open(rec.TokenSealed); err != nil {
			return nil, errors.New("市场的 token 解不开（hub 的 secret.key 换过？）：重新填一次")
		}
	}
	return mk, nil
}

// marketRead reads a market's index now and keeps it (or why it could not).
func (h *Hub) marketRead(ctx context.Context, id string) (MarketView, error) {
	ms := h.marketsState()
	ms.mu.Lock()
	s := ms.byID[id]
	ms.mu.Unlock()
	if s == nil {
		return MarketView{}, errNoMarket
	}
	mk, err := h.market(s.rec)
	var ix *plugin.MarketIndex
	var warnings []string
	if err == nil {
		ix, warnings, err = mk.Fetch(ctx)
	}
	now := time.Now()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	s = ms.byID[id]
	if s == nil {
		return MarketView{}, errNoMarket
	}
	rec := s.rec
	rec.FetchedAt = &now
	if err != nil {
		rec.Error = err.Error()
	} else {
		rec.Error, rec.Name, rec.Warnings = "", ix.Name, warnings
		rec.Index = mustJSON(ix)
		s.index = ix
	}
	if perr := h.marketStore().PutPluginMarket(ctx, rec); perr != nil && err == nil {
		err = perr
	}
	s.rec = rec
	return marketView(s), err
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

var errNoMarket = errors.New("没有这个市场")

// marketsDir is where git markets' clones are kept.
func (h *Hub) marketsDir() string { return filepath.Join(h.plugins.dir, "plugins", "markets") }

func marketView(s *marketState) MarketView {
	v := MarketView{ID: s.rec.ID, Spec: s.rec.Spec, Name: s.rec.Name, HasToken: s.rec.TokenSealed != "", Warnings: s.rec.Warnings,
		Error: s.rec.Error, FetchedAt: s.rec.FetchedAt, AddedAt: s.rec.AddedAt}
	if v.Warnings == nil {
		v.Warnings = []string{}
	}
	if s.index != nil {
		v.Description, v.Homepage, v.Plugins = s.index.Description, s.index.Homepage, len(s.index.Plugins)
	}
	if v.Name == "" {
		v.Name = s.rec.Spec
	}
	return v
}

func (h *Hub) marketViews() []MarketView {
	ms := h.marketsState()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := []MarketView{}
	for _, s := range ms.byID {
		out = append(out, marketView(s))
	}
	slices.SortFunc(out, func(a, b MarketView) int { return a.AddedAt.Compare(b.AddedAt) })
	return out
}

// marketAdd registers a market after reading its index once (a market that cannot be read is not added).
func (h *Hub) marketAdd(ctx context.Context, specText, token string) (MarketView, error) {
	if err := h.marketsLoad(ctx); err != nil {
		return MarketView{}, err
	}
	spec, err := plugin.ParseMarket(specText)
	if err != nil {
		return MarketView{}, err
	}
	if len(token) > plugin.MarketTokenMost {
		return MarketView{}, errors.New("token 太长")
	}
	id := plugin.MarketID(spec)
	ms := h.marketsState()
	ms.mu.Lock()
	n, dup := len(ms.byID), ms.byID[id] != nil
	ms.mu.Unlock()
	if dup {
		return MarketView{}, errors.New("已经登记过这个市场")
	}
	if n >= plugin.MarketsMost {
		return MarketView{}, errors.New("市场太多了：先删掉几个")
	}
	mk := &plugin.Market{Spec: spec, Token: token, Client: ms.client, GitHubAPI: ms.githubAPI, Dir: h.marketsDir()}
	ix, warnings, err := mk.Fetch(ctx)
	if err != nil {
		return MarketView{}, err
	}
	now := time.Now()
	rec := plugin.MarketRecord{ID: id, Spec: spec.String(), Name: ix.Name, Index: mustJSON(ix), Warnings: warnings, FetchedAt: &now, AddedAt: now}
	if token != "" {
		if rec.TokenSealed, err = (hubSealer{h}).Seal(token); err != nil {
			return MarketView{}, err
		}
	}
	if err := h.marketStore().PutPluginMarket(ctx, rec); err != nil {
		return MarketView{}, err
	}
	s := &marketState{rec: rec, index: ix}
	ms.mu.Lock()
	ms.byID[id] = s
	ms.mu.Unlock()
	h.logf("plugin market %s added: %s (%d plugins)", id, rec.Spec, len(ix.Plugins))
	return marketView(s), nil
}

// catalog is every market's plugins, filtered by q (in the id, name, description, publisher, tags) and market.
func (h *Hub) catalog(q, market string) []CatalogItem {
	ms := h.marketsState()
	ms.mu.Lock()
	states := make([]*marketState, 0, len(ms.byID))
	for _, s := range ms.byID {
		states = append(states, s)
	}
	ms.mu.Unlock()
	slices.SortFunc(states, func(a, b *marketState) int { return a.rec.AddedAt.Compare(b.rec.AddedAt) })
	q = strings.ToLower(strings.TrimSpace(q))
	out := []CatalogItem{}
	for _, s := range states {
		if s.index == nil || (market != "" && market != s.rec.ID) {
			continue
		}
		for i := range s.index.Plugins {
			e := &s.index.Plugins[i]
			m := e.Parsed
			if q != "" && !strings.Contains(strings.ToLower(strings.Join(append([]string{m.ID, m.Name, m.Description, m.Publisher}, e.Tags...), "\n")), q) {
				continue
			}
			it := CatalogItem{Market: s.rec.ID, MarketName: s.rec.Name, ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description,
				Publisher: m.Publisher, Homepage: m.Homepage, Tags: append([]string{}, e.Tags...), Contributes: m.Contributions(), Permissions: plugin.Explain(m)}
			if e.PackageFor(plugin.Platform()) == nil {
				it.Problem = "没有这台 hub（" + plugin.Platform() + "）能用的包"
			} else if _, err := plugin.CheckEngines(m, h.build.Number); err != nil {
				it.Problem = err.Error()
			}
			if cur := h.plugins.reg.Get(m.ID); cur != nil {
				it.Installed, it.InstalledSource = cur.Record.Version, cur.Record.Source
				it.FromThisMarket = plugin.FromMarket(cur.Record.Origin) == s.rec.ID
				it.Upgradable = cur.Record.Source == plugin.SourceInstalled && plugin.NewerVersion(m.Version, cur.Record.Version)
			}
			out = append(out, it)
		}
	}
	return out
}

// refreshStale reads again, in the background, the indexes older than marketStale (one read per market at a time).
func (h *Hub) refreshStale() {
	ms := h.marketsState()
	ms.mu.Lock()
	var ids []string
	for id, s := range ms.byID {
		if ms.reading[id] || (s.rec.FetchedAt != nil && time.Since(*s.rec.FetchedAt) < marketStale) {
			continue
		}
		if ms.reading == nil {
			ms.reading = map[string]bool{}
		}
		ms.reading[id] = true
		ids = append(ids, id)
	}
	ms.mu.Unlock()
	for _, id := range ids {
		go func() {
			defer func() { ms.mu.Lock(); delete(ms.reading, id); ms.mu.Unlock() }()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			if _, err := h.marketRead(ctx, id); err != nil {
				h.logf("plugin market %s: %v", id, err)
			}
		}()
	}
}

// marketEntry is a market's entry for plugin id.
func (h *Hub) marketEntry(market, id string) (plugin.MarketRecord, *plugin.MarketEntry, error) {
	ms := h.marketsState()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	s := ms.byID[market]
	if s == nil {
		return plugin.MarketRecord{}, nil, errNoMarket
	}
	if s.index != nil {
		for i := range s.index.Plugins {
			if e := &s.index.Plugins[i]; e.Parsed.ID == id {
				return s.rec, e, nil
			}
		}
	}
	return plugin.MarketRecord{}, nil, errors.New("这个市场里没有插件 " + id)
}

// marketRoutes are the markets' routes for the apps (a device with full rights).
func (h *Hub) marketRoutes(api *http.ServeMux) {
	h.marketRoutesAt(api, "/api/v1/plugins/-", func(w http.ResponseWriter, r *http.Request) bool {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return false
		}
		return true
	})
}

// marketRoutesAt registers the markets' routes under base (the apps' API, or the admin UI's behind its session).
func (h *Hub) marketRoutesAt(api *http.ServeMux, base string, allow func(w http.ResponseWriter, r *http.Request) bool) {
	gate := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !allow(w, r) {
				return
			}
			if err := h.marketsLoad(r.Context()); err != nil {
				writeErrCode(w, http.StatusServiceUnavailable, "no_markets", err)
				return
			}
			f(w, r)
		}
	}
	fail := func(w http.ResponseWriter, err error, code string) {
		if errors.Is(err, errNoMarket) {
			writeErrCode(w, http.StatusNotFound, "not_found", err)
			return
		}
		writeErrCode(w, http.StatusBadRequest, code, err)
	}
	api.HandleFunc("GET "+base+"/markets", gate(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"markets": h.marketViews()})
	}))
	api.HandleFunc("POST "+base+"/markets", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Spec  string `json:"spec"`
			Token string `json:"token,omitempty"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		v, err := h.marketAdd(r.Context(), in.Spec, in.Token)
		if err != nil {
			fail(w, err, "market_failed")
			return
		}
		writeJSON(w, v)
	}))
	api.HandleFunc("DELETE "+base+"/markets/{mid}", gate(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("mid")
		ms := h.marketsState()
		ms.mu.Lock()
		s, ok := ms.byID[id]
		ms.mu.Unlock()
		if !ok {
			fail(w, errNoMarket, "")
			return
		}
		if mk, err := h.market(s.rec); err == nil {
			mk.RemoveCheckout()
		}
		if err := h.marketStore().DeletePluginMarket(r.Context(), id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		ms.mu.Lock()
		delete(ms.byID, id)
		ms.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("POST "+base+"/markets/{mid}/refresh", gate(func(w http.ResponseWriter, r *http.Request) {
		v, err := h.marketRead(r.Context(), r.PathValue("mid"))
		if err != nil {
			fail(w, err, "market_unreachable")
			return
		}
		writeJSON(w, v)
	}))
	api.HandleFunc("PUT "+base+"/markets/{mid}/token", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		if len(in.Token) > plugin.MarketTokenMost {
			fail(w, errors.New("token 太长"), "invalid_token")
			return
		}
		id := r.PathValue("mid")
		sealed := ""
		if in.Token != "" {
			var err error
			if sealed, err = (hubSealer{h}).Seal(in.Token); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		ms := h.marketsState()
		ms.mu.Lock()
		s := ms.byID[id]
		if s != nil {
			s.rec.TokenSealed = sealed
		}
		ms.mu.Unlock()
		if s == nil {
			fail(w, errNoMarket, "")
			return
		}
		v, err := h.marketRead(r.Context(), id)
		if err != nil {
			fail(w, err, "market_unreachable")
			return
		}
		writeJSON(w, v)
	}))
	api.HandleFunc("GET "+base+"/catalog", gate(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("refresh") == "1" {
			var wg sync.WaitGroup
			for _, v := range h.marketViews() {
				wg.Add(1)
				go func() { defer wg.Done(); _, _ = h.marketRead(r.Context(), v.ID) }()
			}
			wg.Wait()
		} else {
			h.refreshStale()
		}
		writeJSON(w, map[string]any{"markets": h.marketViews(), "plugins": h.catalog(r.URL.Query().Get("q"), r.URL.Query().Get("market")),
			"platform": plugin.Platform()})
	}))
	api.HandleFunc("POST "+base+"/catalog/preview", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Market string `json:"market"`
			Plugin string `json:"plugin"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		rec, e, err := h.marketEntry(in.Market, in.Plugin)
		if err != nil {
			fail(w, err, "not_in_market")
			return
		}
		mk, err := h.market(rec)
		if err != nil {
			fail(w, err, "market_failed")
			return
		}
		upgrade := ""
		if cur := h.plugins.reg.Get(in.Plugin); cur != nil && cur.Record.Source == plugin.SourceInstalled {
			upgrade = in.Plugin
		}
		pv, err := h.plugins.reg.PreviewMarket(r.Context(), mk, rec.ID, e, upgrade)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_plugin", err)
			return
		}
		writeJSON(w, pv)
	}))
}
