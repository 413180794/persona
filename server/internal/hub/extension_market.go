package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Plugin markets (docs/extensions.md「扩展市场」): the hub keeps the markets the user registered and their indexes, lists
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
	rec   extension.MarketRecord
	index *extension.MarketIndex
}

// MarketView is a market as the apps list it.
type MarketView struct {
	ID          string     `json:"id"`
	Spec        string     `json:"spec"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Homepage    string     `json:"homepage,omitempty"`
	HasToken    bool       `json:"has_token"`
	Extensions  int        `json:"extensions"`
	Warnings    []string   `json:"warnings"`
	Error       string     `json:"error,omitempty"`
	FetchedAt   *time.Time `json:"fetched_at,omitempty"`
	AddedAt     time.Time  `json:"added_at"`
	// Official: the market every hub has without adding it (extension.Official), listed first.
	Official bool `json:"official,omitempty"`
	// Signature is the index's signature (docs/extensions.md「签名」): signed / unsigned / invalid / key_changed, and the
	// key it is signed by.
	Signature MarketSignatureView `json:"signature"`
}

// MarketSignatureView is a market's signature as the apps show it.
type MarketSignatureView struct {
	State string `json:"state"`
	KeyID string `json:"key_id,omitempty"`
}

// errMarketSignature: a market's index was read but its signature refuses it (the last good one stays).
var errMarketSignature = errors.New("market signature")

// signedIndex judges what was read of market rec against the key it trusts: the record to keep and the index to use
// (nil: keep the last good one), or why it is refused.
func signedIndex(rec extension.MarketRecord, f *extension.Fetched, now time.Time) (extension.MarketRecord, *extension.MarketIndex, error) {
	sig, trust, why := extension.CheckSignature(f.Raw, f.Sig, rec.SigningKey, f.Index.SigningKey)
	rec.FetchedAt, rec.Signature, rec.SigningKey = &now, sig, trust
	if why != "" {
		rec.Error = why
		return rec, nil, fmt.Errorf("%w: %s", errMarketSignature, why)
	}
	rec.Error, rec.Name, rec.Warnings, rec.Index = "", f.Index.Name, f.Warnings, mustJSON(f.Index)
	return rec, f.Index, nil
}

// CatalogItem is a market's plugin as the catalog shows it, with what this hub has of it.
type CatalogItem struct {
	Market      string                     `json:"market"`
	MarketName  string                     `json:"market_name"`
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version"`
	Description string                     `json:"description,omitempty"`
	Publisher   string                     `json:"publisher,omitempty"`
	Homepage    string                     `json:"homepage,omitempty"`
	Tags        []string                   `json:"tags"`
	Contributes map[string][]string        `json:"contributes"`
	Permissions []extension.PermissionLine `json:"permissions"`
	// Problem: why this hub cannot install it (no package for its platform, a newer hub needed); "" when it can.
	Problem string `json:"problem,omitempty"`
	// Installed is the version this hub has ("" none); InstalledSource where that one is from (builtin, hub.json,
	// installed); FromThisMarket whether it was installed from this market; Upgradable whether this one is newer.
	Installed       string `json:"installed,omitempty"`
	InstalledSource string `json:"installed_source,omitempty"`
	FromThisMarket  bool   `json:"from_this_market,omitempty"`
	Upgradable      bool   `json:"upgradable,omitempty"`
}

func (h *Hub) marketStore() extension.MarketStore {
	s, _ := h.store.(extension.MarketStore)
	return s
}

func (h *Hub) marketsState() *markets {
	if h.extensions == nil {
		return nil
	}
	return &h.extensions.markets
}

// load reads the markets from the store once.
func (h *Hub) marketsLoad(ctx context.Context) error {
	ms, st := h.marketsState(), h.marketStore()
	if ms == nil || st == nil {
		return i18n.Err("hub-extension_market.no_database")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.loaded {
		return nil
	}
	rows, err := st.ExtensionMarkets(ctx)
	if err != nil {
		return err
	}
	ms.byID = map[string]*marketState{}
	for _, r := range rows {
		s := &marketState{rec: r}
		if len(r.Index) > 0 {
			s.index, _, _ = extension.ParseIndex(r.Index)
		}
		ms.byID[r.ID] = s
	}
	ms.loaded = true
	if id := h.seedOfficialMarket(ctx); id != "" {
		ms.readLater(h, id) // its index, read in the background
	}
	return nil
}

// officialMarketKey is the setting that says the official market was added (its spec): once only, so a market its
// owner removed is not added again; a new official spec is added once more.
const officialMarketKey = "extension_official_market"

// seedOfficialMarket adds the official market to a hub that has not had it yet (a new one, or one upgraded from before
// #2351), with its pinned key; a market of the same repository the owner already added counts as it (ms.mu held).
// It is the id of the one it added ("" none).
func (h *Hub) seedOfficialMarket(ctx context.Context) string {
	o := extension.Official
	if o.Spec == "" {
		return ""
	}
	var done struct {
		Spec string `json:"spec"`
	}
	if found, err := h.store.Setting(ctx, officialMarketKey, &done); err != nil || (found && done.Spec == o.Spec) {
		return ""
	}
	ms, st := h.marketsState(), h.marketStore()
	mark := func() {
		done.Spec = o.Spec
		if err := h.store.PutSetting(ctx, officialMarketKey, done); err != nil {
			h.logf("extension official market: %v", err)
		}
	}
	for _, s := range ms.byID {
		if o.Is(s.rec.Spec) {
			mark()
			return ""
		}
	}
	spec, err := extension.ParseMarket(o.Spec)
	if err != nil {
		return ""
	}
	rec := extension.MarketRecord{ID: extension.MarketID(spec), Spec: spec.String(), SigningKey: o.SigningKey, AddedAt: time.Now(),
		Warnings: []string{}}
	if err := st.PutExtensionMarket(ctx, rec); err != nil {
		h.logf("extension official market: %v", err)
		return ""
	}
	ms.byID[rec.ID] = &marketState{rec: rec}
	mark()
	h.logf("extension market %s added: %s (official, key %s)", rec.ID, rec.Spec, keyIDOf(rec.SigningKey))
	return rec.ID
}

// marketBefore orders markets: the official one first, then by when they were added.
func marketBefore(a, b extension.MarketRecord) int {
	if oa, ob := extension.Official.Is(a.Spec), extension.Official.Is(b.Spec); oa != ob {
		if oa {
			return -1
		}
		return 1
	}
	if c := a.AddedAt.Compare(b.AddedAt); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

func (h *Hub) market(rec extension.MarketRecord) (*extension.Market, error) {
	spec, err := extension.ParseMarket(rec.Spec)
	if err != nil {
		return nil, err
	}
	mk := &extension.Market{Spec: spec, TrustedKey: rec.SigningKey, Client: h.extensions.markets.client, GitHubAPI: h.extensions.markets.githubAPI, Dir: h.marketsDir()}
	if rec.TokenSealed != "" {
		if mk.Token, err = (hubSealer{h}).Open(rec.TokenSealed); err != nil {
			return nil, i18n.Err("hub-extension_market.token_unreadable")
		}
	}
	return mk, nil
}

// marketRead reads a market's index now and keeps it (or why it could not). An index its signature refuses is not
// taken: the last good one stays, and the market says why (docs/extensions.md「签名」).
func (h *Hub) marketRead(ctx context.Context, id string) (MarketView, error) {
	return h.marketReadAs(ctx, id, false)
}

// marketTrust reads market id again and accepts what its signature now is: a new key (key_changed, signed by the key
// its index names), or no signature any more. A signature that does not verify is never accepted.
func (h *Hub) marketTrust(ctx context.Context, id string) (MarketView, error) {
	return h.marketReadAs(ctx, id, true)
}

func (h *Hub) marketReadAs(ctx context.Context, id string, trust bool) (MarketView, error) {
	ms := h.marketsState()
	ms.mu.Lock()
	s := ms.byID[id]
	ms.mu.Unlock()
	if s == nil {
		return MarketView{}, errNoMarket
	}
	mk, err := h.market(s.rec)
	var f *extension.Fetched
	if err == nil {
		f, err = mk.FetchSigned(ctx)
	}
	now := time.Now()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	s = ms.byID[id]
	if s == nil {
		return MarketView{}, errNoMarket
	}
	rec := s.rec
	if err != nil {
		rec.FetchedAt, rec.Error = &now, err.Error()
	} else {
		if trust {
			sig, _, _ := extension.CheckSignature(f.Raw, f.Sig, rec.SigningKey, f.Index.SigningKey)
			switch {
			case sig.State == extension.SigKeyChanged && sig.Pending != "":
				h.logf("extension market %s: its owner trusts the new key %s (was %s)", id, sig.KeyID, keyIDOf(rec.SigningKey))
				rec.SigningKey = sig.Pending
			case sig.State == extension.SigInvalid && sig.Unsign:
				h.logf("extension market %s: its owner accepts it unsigned (was %s)", id, keyIDOf(rec.SigningKey))
				rec.SigningKey = ""
			case sig.Bad():
				return marketView(s), i18n.Err("hub-extension_market.no_trusted_key")
			}
		}
		var ix *extension.MarketIndex
		if rec, ix, err = signedIndex(rec, f, now); ix != nil {
			s.index = ix
		} else {
			h.logf("extension market %s: %v", id, err)
		}
	}
	if perr := h.marketStore().PutExtensionMarket(ctx, rec); perr != nil && err == nil {
		err = perr
	}
	s.rec = rec
	return marketView(s), err
}

func keyIDOf(key string) string {
	k, err := extension.ParseSigningKey(key)
	if err != nil {
		return "无"
	}
	return extension.KeyID(k)
}

// signatureText is a market's signature for the log.
func signatureText(s extension.MarketSignature) string {
	if s.State == extension.SigSigned {
		return "signed by " + s.KeyID
	}
	return s.State
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

var errNoMarket = i18n.Err("hub-extension_market.no_market")

// marketsDir is where git markets' clones are kept.
func (h *Hub) marketsDir() string { return filepath.Join(h.extensions.root, "markets") }

func marketView(s *marketState) MarketView {
	v := MarketView{ID: s.rec.ID, Spec: s.rec.Spec, Name: s.rec.Name, HasToken: s.rec.TokenSealed != "", Warnings: s.rec.Warnings,
		Error: s.rec.Error, FetchedAt: s.rec.FetchedAt, AddedAt: s.rec.AddedAt, Official: extension.Official.Is(s.rec.Spec),
		Signature: MarketSignatureView{State: s.rec.Signature.State, KeyID: s.rec.Signature.KeyID}}
	if v.Signature.State == "" { // a market read before #1848
		v.Signature.State = extension.SigUnsigned
	}
	if v.Warnings == nil {
		v.Warnings = []string{}
	}
	if s.index != nil {
		v.Description, v.Homepage, v.Extensions = s.index.Description, s.index.Homepage, len(s.index.Extensions)
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
	states := make([]*marketState, 0, len(ms.byID))
	for _, s := range ms.byID {
		states = append(states, s)
	}
	slices.SortFunc(states, func(a, b *marketState) int { return marketBefore(a.rec, b.rec) })
	out := []MarketView{}
	for _, s := range states {
		out = append(out, marketView(s))
	}
	return out
}

// marketAdd registers a market after reading its index once (a market that cannot be read is not added).
func (h *Hub) marketAdd(ctx context.Context, specText, token string) (MarketView, error) {
	if err := h.marketsLoad(ctx); err != nil {
		return MarketView{}, err
	}
	spec, err := extension.ParseMarket(specText)
	if err != nil {
		return MarketView{}, err
	}
	if len(token) > extension.MarketTokenMost {
		return MarketView{}, i18n.Err("hub-extension_market.token_too_long")
	}
	id := extension.MarketID(spec)
	ms := h.marketsState()
	ms.mu.Lock()
	n, dup := len(ms.byID), ms.byID[id] != nil || ms.withSpec(spec.String()) != nil || (extension.Official.Is(spec.String()) && ms.official() != nil)
	ms.mu.Unlock()
	if dup {
		return MarketView{}, i18n.Err("hub-extension_market.duplicate")
	}
	if n >= extension.MarketsMost {
		return MarketView{}, i18n.Err("hub-extension_market.too_many")
	}
	mk := &extension.Market{Spec: spec, Token: token, Client: ms.client, GitHubAPI: ms.githubAPI, Dir: h.marketsDir()}
	f, err := mk.FetchSigned(ctx)
	if err != nil {
		return MarketView{}, err
	}
	now := time.Now()
	// The first read is trust on first use: a signed market's key is kept from now on.
	rec, ix, err := signedIndex(extension.MarketRecord{ID: id, Spec: spec.String(), AddedAt: now}, f, now)
	if err != nil {
		mk.RemoveCheckout()
		return MarketView{}, err
	}
	if token != "" {
		if rec.TokenSealed, err = (hubSealer{h}).Seal(token); err != nil {
			return MarketView{}, err
		}
	}
	if err := h.marketStore().PutExtensionMarket(ctx, rec); err != nil {
		return MarketView{}, err
	}
	s := &marketState{rec: rec, index: ix}
	ms.mu.Lock()
	ms.byID[id] = s
	ms.mu.Unlock()
	h.logf("extension market %s added: %s (%d extensions, %s)", id, rec.Spec, len(ix.Extensions), signatureText(rec.Signature))
	return marketView(s), nil
}

// marketToken is the token market id was registered with (empty when it has none).
func (h *Hub) marketToken(id string) (string, error) {
	ms := h.marketsState()
	ms.mu.Lock()
	s := ms.byID[id]
	ms.mu.Unlock()
	if s == nil {
		return "", errNoMarket
	}
	if s.rec.TokenSealed == "" {
		return "", i18n.Err("hub-extension_market.no_token")
	}
	mk, err := h.market(s.rec)
	if err != nil {
		return "", err
	}
	return mk.Token, nil
}

// official is the registered official market (ms.mu held).
func (ms *markets) official() *marketState {
	for _, s := range ms.byID {
		if extension.Official.Is(s.rec.Spec) {
			return s
		}
	}
	return nil
}

// withSpec is the market registered at spec (ms.mu held).
func (ms *markets) withSpec(spec string) *marketState {
	for _, s := range ms.byID {
		if s.rec.Spec == spec {
			return s
		}
	}
	return nil
}

// marketSetSpec moves market id to another address, keeping its id: what was installed from it (source
// market:<id>/<extension>) stays its own, upgrades and all. The new address is read once first, with the market's
// token; one that cannot be read changes nothing.
func (h *Hub) marketSetSpec(ctx context.Context, id, specText string) (MarketView, error) {
	if err := h.marketsLoad(ctx); err != nil {
		return MarketView{}, err
	}
	spec, err := extension.ParseMarket(specText)
	if err != nil {
		return MarketView{}, err
	}
	ms := h.marketsState()
	ms.mu.Lock()
	s := ms.byID[id]
	other := ms.withSpec(spec.String())
	ms.mu.Unlock()
	if s == nil {
		return MarketView{}, errNoMarket
	}
	if other != nil && other != s {
		return MarketView{}, i18n.Err("hub-extension_market.address_taken")
	}
	old, err := h.market(s.rec)
	if err != nil {
		return MarketView{}, err
	}
	mk := &extension.Market{Spec: spec, Token: old.Token, Client: ms.client, GitHubAPI: ms.githubAPI, Dir: h.marketsDir()}
	f, err := mk.FetchSigned(ctx)
	if err != nil {
		return MarketView{}, err
	}
	now := time.Now()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if s = ms.byID[id]; s == nil {
		return MarketView{}, errNoMarket
	}
	// The new address is held to the key the market is trusted with: a move is not a way to change it.
	rec, ix, err := signedIndex(s.rec, f, now)
	if err != nil {
		if old.Spec.String() != spec.String() {
			mk.RemoveCheckout()
		}
		return MarketView{}, err
	}
	rec.Spec = spec.String()
	if err := h.marketStore().PutExtensionMarket(ctx, rec); err != nil {
		return MarketView{}, err
	}
	if old.Spec.String() != spec.String() {
		old.RemoveCheckout()
	}
	s.rec, s.index = rec, ix
	h.logf("extension market %s moved: %s", id, rec.Spec)
	return marketView(s), nil
}

// catalog is every market's plugins, filtered by q (in the id, name, description, publisher, tags) and market.
func (h *Hub) catalog(q, market string) []CatalogItem { return h.catalogIn(h.userLang(), q, market) }

// catalogIn is catalog with its problems in l.
func (h *Hub) catalogIn(l i18n.Lang, q, market string) []CatalogItem {
	ms := h.marketsState()
	ms.mu.Lock()
	states := make([]*marketState, 0, len(ms.byID))
	for _, s := range ms.byID {
		states = append(states, s)
	}
	ms.mu.Unlock()
	slices.SortFunc(states, func(a, b *marketState) int { return marketBefore(a.rec, b.rec) })
	q = strings.ToLower(strings.TrimSpace(q))
	out := []CatalogItem{}
	for _, s := range states {
		if s.index == nil || (market != "" && market != s.rec.ID) {
			continue
		}
		for i := range s.index.Extensions {
			e := &s.index.Extensions[i]
			m := e.Parsed
			if q != "" && !strings.Contains(strings.ToLower(strings.Join(append([]string{m.ID, m.Name, m.Description, m.Publisher}, e.Tags...), "\n")), q) {
				continue
			}
			it := CatalogItem{Market: s.rec.ID, MarketName: s.rec.Name, ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description,
				Publisher: m.Publisher, Homepage: m.Homepage, Tags: append([]string{}, e.Tags...), Contributes: m.Contributions(), Permissions: extension.Explain(m)}
			if s.rec.Signature.Bad() {
				it.Problem = i18n.T(l, "hub-extension_market.bad_signature", i18n.Args{"err": s.rec.Error})
			} else if e.PackageFor(extension.Platform()) == nil {
				it.Problem = i18n.T(l, "hub-extension_market.no_package", i18n.Args{"platform": extension.Platform()})
			} else if _, err := extension.CheckEngines(m, h.build.Number); err != nil {
				it.Problem = err.Error()
			}
			if cur := h.extensions.reg.Get(m.ID); cur != nil {
				it.Installed, it.InstalledSource = cur.Record.Version, cur.Record.Source
				it.FromThisMarket = extension.FromMarket(cur.Record.Origin) == s.rec.ID
				it.Upgradable = cur.Record.Source == extension.SourceInstalled && extension.NewerVersion(m.Version, cur.Record.Version)
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
	defer ms.mu.Unlock()
	for id, s := range ms.byID {
		if s.rec.FetchedAt == nil || time.Since(*s.rec.FetchedAt) >= marketStale {
			ms.readLater(h, id)
		}
	}
}

// readLater reads market id again in the background, unless it is being read already (ms.mu held).
func (ms *markets) readLater(h *Hub, id string) {
	if ms.reading[id] {
		return
	}
	if ms.reading == nil {
		ms.reading = map[string]bool{}
	}
	ms.reading[id] = true
	go func() {
		defer func() { ms.mu.Lock(); delete(ms.reading, id); ms.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		if _, err := h.marketRead(ctx, id); err != nil {
			h.logf("plugin market %s: %v", id, err)
		}
	}()
}

// marketEntry is a market's entry for plugin id.
func (h *Hub) marketEntry(market, id string) (extension.MarketRecord, *extension.MarketEntry, error) {
	ms := h.marketsState()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return marketEntryLocked(ms, market, id)
}

func marketEntryLocked(ms *markets, market, id string) (extension.MarketRecord, *extension.MarketEntry, error) {
	s := ms.byID[market]
	if s == nil {
		return extension.MarketRecord{}, nil, errNoMarket
	}
	if s.rec.Signature.Bad() {
		// Nothing is installed or upgraded from a market whose signature fails, not even from its last good index.
		return extension.MarketRecord{}, nil, i18n.Err("hub-extension_market.bad_signature_install", i18n.Args{"err": s.rec.Error})
	}
	if s.index != nil {
		for i := range s.index.Extensions {
			if e := &s.index.Extensions[i]; e.Parsed.ID == id {
				return s.rec, e, nil
			}
		}
	}
	return extension.MarketRecord{}, nil, i18n.Err("hub-extension_market.not_in_market", i18n.Args{"id": id})
}

// marketRoutes are the markets' routes for the apps (a device with full rights).
func (h *Hub) marketRoutes(api *http.ServeMux) {
	h.marketRoutesAt(api, "/api/v1/extensions/-", func(w http.ResponseWriter, r *http.Request) bool {
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
		writeJSON(w, map[string]any{"markets": marketsOut(r, h.marketViews())})
	}))
	api.HandleFunc("POST "+base+"/markets", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Spec  string `json:"spec"`
			Token string `json:"token,omitempty"`
			// TokenFrom: use the token of this registered market (one account's token reading several private
			// repositories), never seen by the caller.
			TokenFrom string `json:"token_from,omitempty"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if in.TokenFrom != "" {
			if in.Token != "" {
				fail(w, i18n.Err("hub-extension_market.token_and_token_from"), "market_failed")
				return
			}
			tok, err := h.marketToken(in.TokenFrom)
			if err != nil {
				fail(w, err, "market_failed")
				return
			}
			in.Token = tok
		}
		v, err := h.marketAdd(r.Context(), in.Spec, in.Token)
		if err != nil {
			fail(w, err, "market_failed")
			return
		}
		writeJSON(w, marketOut(r, v))
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
		if err := h.marketStore().DeleteExtensionMarket(r.Context(), id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		ms.mu.Lock()
		delete(ms.byID, id)
		ms.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("PUT "+base+"/markets/{mid}", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Spec string `json:"spec"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		v, err := h.marketSetSpec(r.Context(), r.PathValue("mid"), in.Spec)
		if err != nil {
			fail(w, err, "market_failed")
			return
		}
		writeJSON(w, marketOut(r, v))
	}))
	api.HandleFunc("POST "+base+"/markets/{mid}/refresh", gate(func(w http.ResponseWriter, r *http.Request) {
		v, err := h.marketRead(r.Context(), r.PathValue("mid"))
		if errors.Is(err, errMarketSignature) {
			fail(w, err, "market_signature")
			return
		}
		if err != nil {
			fail(w, err, "market_unreachable")
			return
		}
		writeJSON(w, marketOut(r, v))
	}))
	api.HandleFunc("POST "+base+"/markets/{mid}/trust", gate(func(w http.ResponseWriter, r *http.Request) {
		v, err := h.marketTrust(r.Context(), r.PathValue("mid"))
		if err != nil {
			fail(w, err, "market_signature")
			return
		}
		writeJSON(w, marketOut(r, v))
	}))
	api.HandleFunc("PUT "+base+"/markets/{mid}/token", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if len(in.Token) > extension.MarketTokenMost {
			fail(w, i18n.Err("hub-extension_market.token_too_long"), "invalid_token")
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
		writeJSON(w, marketOut(r, v))
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
		writeJSON(w, map[string]any{"markets": marketsOut(r, h.marketViews()), extKey(r, "extensions", "plugins"): h.catalogIn(h.requestLang(r), r.URL.Query().Get("q"), r.URL.Query().Get("market")),
			"platform": extension.Platform()})
	}))
	api.HandleFunc("POST "+base+"/catalog/preview", gate(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Market    string `json:"market"`
			Extension string `json:"extension"`
			Legacy    string `json:"plugin"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if in.Extension == "" {
			in.Extension = in.Legacy
		}
		rec, e, err := h.marketEntry(in.Market, in.Extension)
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
		if cur := h.extensions.reg.Get(in.Extension); cur != nil && cur.Record.Source == extension.SourceInstalled {
			upgrade = in.Extension
		}
		pv, err := h.extensions.reg.PreviewMarket(r.Context(), mk, rec.ID, e, upgrade)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_plugin", err)
			return
		}
		h.resolveBundle(r.Context(), pv, rec.ID) // its missing dependencies, a pack's members (extension_model.go)
		writeJSON(w, pv)
	}))
}
