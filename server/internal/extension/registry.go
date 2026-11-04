package extension

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Where a plugin comes from.
const (
	SourceBuiltin   = "builtin"   // compiled into personad; a row keeps its switch and settings
	SourceHubJSON   = "hub.json"  // hub.json's plugins[]: read-only here, never in the database
	SourceInstalled = "installed" // a package installed on the hub's machine, or a remote plugin
)

// SecretMost is the largest secret value kept.
const SecretMost = 16 << 10

// previewFor is how long a preview's staged package is kept.
const previewFor = 10 * time.Minute

// Record is a plugin's row (table plugins): what the user installed and chose.
type Record struct {
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Origin          string          `json:"origin,omitempty"`
	Version         string          `json:"version"`
	PreviousVersion string          `json:"previous_version,omitempty"`
	Manifest        json.RawMessage `json:"manifest"`
	Granted         Permissions     `json:"granted"`
	GrantedDigest   string          `json:"granted_digest,omitempty"`
	Enabled         bool            `json:"enabled"`
	Config          json.RawMessage `json:"config,omitempty"`
	Remote          *Remote         `json:"remote,omitempty"`
	// DisabledContributions are the contributions the user switched off ("<point>/<id>", docs/extensions.md「贡献的开关」):
	// they do not take effect while the rest of the extension runs. A connector's switch is the connector's own.
	DisabledContributions []string  `json:"disabled_contributions,omitempty"`
	InstalledAt           time.Time `json:"installed_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// Remote is a plugin that runs elsewhere and is reached on these sockets.
type Remote struct {
	RPCSocket  string `json:"rpc_socket,omitempty"`
	HTTPSocket string `json:"http_socket,omitempty"`
}

// Store keeps the registry (the hub's Postgres: tables plugins, plugin_secrets, plugin_audit).
type Store interface {
	Extensions(ctx context.Context) ([]Record, error)
	PutExtension(ctx context.Context, r Record) error
	// DeletePlugin deletes the row and its secrets.
	DeleteExtension(ctx context.Context, id string) error
	// PluginSecrets are the sealed secrets by name.
	ExtensionSecrets(ctx context.Context, id string) (map[string]string, error)
	PutExtensionSecret(ctx context.Context, id, name, sealed string) error
	DeleteExtensionSecret(ctx context.Context, id, name string) error
	AddExtensionAudit(ctx context.Context, id, action, actor string, detail json.RawMessage) error
}

// Sealer seals secrets with the hub's secret.key (the same as mail passwords and launchers' keys).
type Sealer interface {
	Seal(plain string) (string, error)
	Open(sealed string) (string, error)
}

// Extension is one registered extension.
type Extension struct {
	Record Record
	// Manifest is what takes effect: the manifest without the contributions the user switched off (Without). Full is
	// the manifest as installed: what the extension may do (permissions, the ids it holds) and every contribution the
	// user can switch.
	Manifest *Manifest
	Full     *Manifest
	Schema   *Schema
	// Dir is the package's directory ("" for a remote or hub.json plugin).
	Dir string
	// Problem is why it cannot run on this hub (engines, a missing package); it is then not started.
	Problem  string
	Warnings []string
	// Legacy is a hub.json plugin's entry, as written (its command is a path on the machine, not in a package).
	Legacy any
}

// ReadOnly: hub.json's, changed there only.
func (p *Extension) ReadOnly() bool { return p.Record.Source == SourceHubJSON }

// Change is what the registry tells the hub after it changed a plugin.
type Change struct {
	ID   string
	Kind string // installed | upgraded | rolled_back | removed | enabled | disabled | config | secret
	// Old is the plugin before the change (removed, upgraded, rolled back).
	Old *Extension
}

// Registry is every plugin the hub knows: built-in, from hub.json and installed.
type Registry struct {
	dir      string // <data dir>/plugins
	store    Store  // nil: nothing is kept (no database), nothing installs
	seal     Sealer
	build    int64
	platform string

	// OnChange is called after each change, outside the registry's lock.
	OnChange func(Change)
	// RemoteManifest asks a remote plugin for its manifest (the hub's host; initialize, or GET /manifest).
	RemoteManifest func(ctx context.Context, r Remote) (*Manifest, error)
	// CanEnable says why a plugin (its source and manifest) may not be on now, nil when it may: asked before it is
	// switched on, installed (on at once) or upgraded while on. A rollback is not asked (it goes back to a version
	// that ran; the hub rolls back a broken upgrade itself).
	CanEnable func(ctx context.Context, source string, m *Manifest) error

	mu         sync.Mutex
	extensions map[string]*Extension
	reserved   map[string]map[string]string // point → contribution id → who has it (the hub's own)
	previews   map[string]*Preview
	now        func() time.Time
	// yields: built-in plugins an installed one of the same id takes the place of (Yields); yielded: those put aside.
	yields  map[string]bool
	yielded map[string]*Extension
	// devSecrets are the secrets of the extensions being written (SourceDev), in memory only.
	devSecrets map[string]map[string]string
}

// NewRegistry is a registry for the hub of build, keeping packages under dir.
func NewRegistry(dir string, store Store, seal Sealer, build int64) *Registry {
	return &Registry{dir: dir, store: store, seal: seal, build: build, platform: runtime.GOOS + "-" + runtime.GOARCH,
		extensions: map[string]*Extension{}, reserved: map[string]map[string]string{}, previews: map[string]*Preview{}, now: time.Now,
		yields: map[string]bool{}, yielded: map[string]*Extension{}}
}

// Yields marks the built-in plugin id as one an installed plugin of the same id takes the place of, while it moves out
// of personad into a package (Telegram, docs/plan-extensions.md): installing that plugin puts the built-in aside (the hub
// stops its program before the installed one starts), uninstalling it brings the built-in back.
func (r *Registry) Yields(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.yields[id] = true
}

// yielding says whether cur is a built-in an installed plugin may take the place of. Called with r.mu held.
func (r *Registry) yielding(cur *Extension) bool {
	return cur != nil && cur.Record.Source == SourceBuiltin && r.yields[cur.Record.ID]
}

// Dirs.
func (r *Registry) installedDir(id, version string) string {
	return filepath.Join(r.dir, "installed", id, version)
}

// DataDir is a plugin's own directory for its caches and data (deleted with it).
func (r *Registry) DataDir(id string) string { return filepath.Join(r.dir, "data", id) }

// HasStore says whether installs are possible.
func (r *Registry) HasStore() bool { return r.store != nil }

// Reserve marks contribution ids the hub has itself (its built-in connectors), so no plugin takes them.
func (r *Registry) Reserve(point string, ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reserved[point] == nil {
		r.reserved[point] = map[string]string{}
	}
	for _, id := range ids {
		r.reserved[point][id] = "the hub"
	}
}

// AddHubJSON registers a hub.json plugin: its manifest made by the hub from the entry (legacy is the entry).
func (r *Registry) AddHubJSON(m *Manifest, legacy any, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extensions[m.ID] != nil {
		return fmt.Errorf("plugin %q: duplicate id", m.ID)
	}
	raw, _ := json.Marshal(m)
	r.extensions[m.ID] = &Extension{Record: Record{ID: m.ID, Source: SourceHubJSON, Version: m.Version, Manifest: raw,
		Granted: m.Permissions, GrantedDigest: Digest(m), Enabled: enabled}, Manifest: m, Full: m, Legacy: legacy}
	return nil
}

// AddBuiltin registers a plugin compiled into personad (its program at dir, if any). Its switch and settings come
// from its row once Load reads it; enabled is the default.
func (r *Registry) AddBuiltin(m *Manifest, dir string, enabled bool) error {
	schema, err := ParseSchema(m.Configuration)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extensions[m.ID] != nil {
		return fmt.Errorf("plugin %q: duplicate id", m.ID)
	}
	raw, _ := json.Marshal(m)
	r.extensions[m.ID] = &Extension{Record: Record{ID: m.ID, Source: SourceBuiltin, Version: m.Version, Manifest: raw,
		Granted: m.Permissions, GrantedDigest: Digest(m), Enabled: enabled}, Manifest: m, Full: m, Schema: schema, Dir: dir}
	return nil
}

// Load reads the rows: installed plugins, and the switches and settings of built-in ones. A row whose package is gone
// or that this hub cannot run is kept and listed with its problem.
func (r *Registry) Load(ctx context.Context) error {
	if r.store == nil {
		return nil
	}
	rows, err := r.store.Extensions(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range rows {
		cur := r.extensions[row.ID]
		switch {
		case row.Source == SourceBuiltin && cur != nil && cur.Record.Source == SourceBuiltin:
			cur.Record.Enabled, cur.Record.Config = row.Enabled, row.Config
			cur.Record.DisabledContributions = row.DisabledContributions
			cur.Manifest = cur.Full.Without(row.DisabledContributions)
			cur.Record.InstalledAt, cur.Record.UpdatedAt = row.InstalledAt, row.UpdatedAt
		case row.Source == SourceInstalled && cur == nil:
			r.extensions[row.ID] = r.fromRow(row)
		case row.Source == SourceInstalled && r.yielding(cur):
			r.yielded[row.ID] = cur
			r.extensions[row.ID] = r.fromRow(row)
		}
	}
	return nil
}

// fromRow is an installed plugin as its row says.
func (r *Registry) fromRow(row Record) *Extension {
	p := &Extension{Record: row}
	m, err := Parse(row.Manifest)
	if err != nil {
		var loose Manifest
		_ = json.Unmarshal(row.Manifest, &loose)
		loose.ID = row.ID
		p.Manifest, p.Full, p.Problem = &loose, &loose, "清单读不了："+err.Error()
		return p
	}
	p.Full, p.Manifest = m, m.Without(row.DisabledContributions)
	p.Schema, _ = ParseSchema(m.Configuration)
	if row.Remote == nil {
		p.Dir = r.installedDir(row.ID, row.Version)
		if !hasManifest(p.Dir) {
			p.Problem = "扩展包不见了（" + p.Dir + "）"
		}
	}
	if w, err := CheckEngines(m, r.build); err != nil {
		p.Problem = err.Error()
	} else if w != "" {
		p.Warnings = append(p.Warnings, w)
	}
	p.Warnings = append(p.Warnings, m.Lint()...)
	return p
}

// List is every plugin, by id.
func (r *Registry) List() []*Extension {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Extension, 0, len(r.extensions))
	for _, p := range r.extensions {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Record.ID < out[j].Record.ID })
	return out
}

// Get is the plugin with id, nil when there is none.
func (r *Registry) Get(id string) *Extension {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.extensions[id]
}

// Preview is a staged package (or a remote plugin) and what installing it means, shown before the user agrees.
type Preview struct {
	ID          string           `json:"preview_id"`
	Manifest    *Manifest        `json:"manifest"`
	Permissions []PermissionLine `json:"permissions"`
	Digest      string           `json:"permissions_digest"`
	Warnings    []string         `json:"warnings,omitempty"`
	ExpiresAt   time.Time        `json:"expires_at"`
	// Upgrade: of the installed plugin with this id, from version From; MorePermissions: it asks for more than the
	// user granted, so the upgrade needs the digest.
	Upgrade         bool   `json:"upgrade,omitempty"`
	From            string `json:"from,omitempty"`
	MorePermissions bool   `json:"more_permissions,omitempty"`
	// AddedPermissions are the lines of what the new version may do that the installed one may not (only what is
	// new: the added hosts, the added secrets), for the upgrade screen.
	AddedPermissions []PermissionLine `json:"added_permissions,omitempty"`
	// Market is the market it comes from (a full-rights device may install it, docs/extensions.md「扩展市场」).
	Market string `json:"market,omitempty"`

	source  Source
	root    string // the staged package
	staging string
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Preview stages a package (or asks a remote plugin) and checks it for installing (upgrade "" ) or for upgrading
// the installed plugin upgrade.
func (r *Registry) Preview(ctx context.Context, source, upgrade string) (*Preview, error) {
	if r.store == nil {
		return nil, errors.New("没有数据库：装不了扩展")
	}
	src, err := ParseSource(source)
	if err != nil {
		return nil, err
	}
	return r.preview(ctx, src, upgrade, func(dir string) (string, error) { return Stage(ctx, src, dir) })
}

// PreviewMarket is Preview for a plugin of a market: its package for this hub's platform, downloaded and checked
// against the index's sha256, must be the plugin the index says (id, version, permissions).
func (r *Registry) PreviewMarket(ctx context.Context, mk *Market, marketID string, e *MarketEntry, upgrade string) (*Preview, error) {
	if r.store == nil {
		return nil, errors.New("没有数据库：装不了扩展")
	}
	pkg := e.PackageFor(Platform())
	if pkg == nil {
		return nil, fmt.Errorf("%s 没有这台 hub（%s）能用的包", e.Parsed.ID, Platform())
	}
	src := Source{Kind: "market", Path: marketID + "/" + e.Parsed.ID}
	pv, err := r.preview(ctx, src, upgrade, func(dir string) (string, error) {
		file, name, err := mk.Download(ctx, pkg, dir)
		if err != nil {
			return "", err
		}
		out := filepath.Join(dir, "pkg")
		if err := unpackAs(file, name, out); err != nil {
			return "", err
		}
		_ = os.Remove(file)
		return packageRoot(out)
	})
	if err != nil {
		return nil, err
	}
	if m := pv.Manifest; m.ID != e.Parsed.ID || m.Version != e.Parsed.Version || Digest(m) != Digest(e.Parsed) {
		if pv, _ := r.take(pv.ID); pv != nil {
			r.dropStaging(pv)
		}
		return nil, fmt.Errorf("下载的包（%s %s）和市场索引里写的（%s %s）不一致", m.ID, m.Version, e.Parsed.ID, e.Parsed.Version)
	}
	pv.Market = marketID
	return pv, nil
}

// MorePermissions says whether m asks for more than the installed cur was granted (its upgrade needs the user's
// confirmation).
func MorePermissions(cur *Extension, m *Manifest) bool {
	was := cur.Full
	return !cur.Record.Granted.Covers(m.Permissions) || (m.Runtime != nil && was.Runtime == nil) || NodeAdds(was, m) ||
		slices.ContainsFunc(m.Topics(), func(t string) bool { return !slices.Contains(was.Topics(), t) }) ||
		slices.ContainsFunc(m.Surfaces(), func(t string) bool { return !slices.Contains(was.Surfaces(), t) })
}

// Discard drops a preview that will not be installed.
func (r *Registry) Discard(previewID string) {
	if pv, _ := r.take(previewID); pv != nil {
		r.dropStaging(pv)
	}
}

// PreviewMarketOf is the market a preview's package comes from ("" when it does not, or the preview is gone).
func (r *Registry) PreviewMarketOf(previewID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pv := r.previews[previewID]; pv != nil {
		return pv.Market
	}
	return ""
}

// PreviewUpload is Preview for a package uploaded in the browser (the admin UI): body is the .tar.gz / .zip named
// name, written into the preview's staging directory (at most PackageMost bytes) and unpacked there.
func (r *Registry) PreviewUpload(ctx context.Context, name string, body io.Reader, upgrade string) (*Preview, error) {
	if r.store == nil {
		return nil, errors.New("没有数据库：装不了扩展")
	}
	name, err := UploadName(name)
	if err != nil {
		return nil, err
	}
	return r.preview(ctx, Source{Kind: "upload", Path: name}, upgrade, func(dir string) (string, error) { return StageUpload(body, name, dir) })
}

// preview checks what stage put into a fresh staging directory (or what a remote plugin says of itself).
func (r *Registry) preview(ctx context.Context, src Source, upgrade string, stage func(dir string) (string, error)) (*Preview, error) {
	r.expire()
	pv := &Preview{ID: newID(), source: src, ExpiresAt: r.now().Add(previewFor)}
	var m *Manifest
	var err error
	if src.Kind == "remote" {
		if r.RemoteManifest == nil {
			return nil, errors.New("this hub cannot reach remote plugins")
		}
		if m, err = r.RemoteManifest(ctx, Remote{RPCSocket: src.RPCSocket, HTTPSocket: src.HTTPSocket}); err != nil {
			return nil, fmt.Errorf("问不到远端扩展的清单：%w", err)
		}
		if err := m.Validate(); err != nil {
			return nil, err
		}
		if err := CheckTriageRules(m); err != nil {
			return nil, err
		}
	} else {
		pv.staging = filepath.Join(r.dir, "staging", pv.ID)
		root, err := stage(pv.staging)
		if err != nil {
			_ = os.RemoveAll(pv.staging)
			return nil, err
		}
		pv.root = root
		data, err := os.ReadFile(ManifestPath(root))
		if err == nil {
			m, err = Parse(data)
		}
		if err == nil {
			err = r.checkPackage(m, root)
		}
		if err == nil {
			err = CheckTriageRules(m)
		}
		if err != nil {
			_ = os.RemoveAll(pv.staging)
			return nil, err
		}
	}
	pv.Manifest, pv.Digest, pv.Permissions = m, Digest(m), Explain(m)
	pv.Warnings = m.Lint()
	fail := func(err error) (*Preview, error) {
		if pv.staging != "" {
			_ = os.RemoveAll(pv.staging)
		}
		return nil, err
	}
	w, err := CheckEngines(m, r.build)
	if err != nil {
		return fail(err)
	}
	if w != "" {
		pv.Warnings = append(pv.Warnings, w)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := r.extensions[m.ID]
	switch {
	case upgrade == "" && cur != nil && !r.yielding(cur):
		return fail(fmt.Errorf("已经有一个叫 %s 的扩展（%s）：要换新版本用升级", m.ID, cur.Record.Source))
	case upgrade != "" && upgrade != m.ID:
		return fail(fmt.Errorf("这个包是 %s，不是 %s", m.ID, upgrade))
	case upgrade != "" && (cur == nil || cur.Record.Source != SourceInstalled):
		return fail(fmt.Errorf("没有装过扩展 %s（内置的和 hub.json 里的不能升级）", upgrade))
	case upgrade != "" && !NewerVersion(m.Version, cur.Record.Version):
		return fail(fmt.Errorf("%s 不比已装的 %s 新", m.Version, cur.Record.Version))
	}
	if err := r.conflicts(m); err != nil {
		return fail(err)
	}
	if upgrade != "" {
		pv.Upgrade, pv.From = true, cur.Record.Version
		pv.MorePermissions = MorePermissions(cur, m)
		pv.AddedPermissions = Added(cur.Manifest, m)
	}
	r.previews[pv.ID] = pv
	return pv, nil
}

// checkPackage holds a staged package to what a manifest says of it: its program is in it for this platform.
func (r *Registry) checkPackage(m *Manifest, root string) error {
	for _, a := range m.Assistants() {
		for _, sk := range a.Skills {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sk), "SKILL.md")); err != nil {
				return fmt.Errorf("assistant %s: %s/SKILL.md 不在包里", a.ID, sk)
			}
		}
		if a.Instructions != "" {
			if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(a.Instructions))); err != nil || !fi.Mode().IsRegular() {
				return fmt.Errorf("assistant %s: %s 不在包里", a.ID, a.Instructions)
			}
		}
		if a.Gate != "" {
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.Gate)))
			if err != nil {
				return fmt.Errorf("assistant %s: %s 不在包里", a.ID, a.Gate)
			}
			g, err := ParseGate(raw)
			if err != nil {
				return fmt.Errorf("assistant %s: %s: %v", a.ID, a.Gate, err)
			}
			// The script is in one of its skills (the skills directory on the assistant's machine is where they go).
			if !slices.ContainsFunc(a.Skills, func(sk string) bool { return strings.HasPrefix(g.Script, path.Base(sk)+"/") }) {
				return fmt.Errorf("assistant %s: the gate's script %s is not in its skills", a.ID, g.Script)
			}
		}
	}
	if m.Node != nil {
		if !NodePrograms {
			return errors.New("这个版本的 hub 还不能在电脑上运行扩展的程序（node 段），先升级 hub")
		}
		// Every computer's program is in the package: the hub sends each machine the one for its platform.
		for _, platform := range slices.Sorted(maps.Keys(m.Node.Command.ByPlatform)) {
			if err := checkProgram(root, "node.command", m.Node.Command.ByPlatform[platform]); err != nil {
				return err
			}
		}
	}
	if m.Runtime == nil {
		return nil
	}
	cmd, ok := m.Runtime.Command.For(r.platform)
	if !ok {
		return fmt.Errorf("这个扩展没有 %s 的程序", r.platform)
	}
	if IsBuiltinCommand(cmd) {
		return errors.New("runtime.command: only a built-in plugin runs the hub's own binary")
	}
	return checkProgram(root, "runtime.command", cmd)
}

// NodePrograms says whether this hub runs plugins' programs on computers (docs/plan-extensions.md「七」): off, a package
// with a node program does not install, rather than install and never run there (a test turns it off).
var NodePrograms = true

// checkProgram holds a program a manifest names (at: which field) to being an executable file inside the package.
func checkProgram(root, at, cmd string) error {
	p := filepath.Join(root, filepath.FromSlash(cmd))
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return fmt.Errorf("%s %s 不在包里", at, cmd)
	}
	rootResolved, _ := filepath.EvalSymlinks(root)
	if rel, err := filepath.Rel(rootResolved, resolved); err != nil || !inside(filepath.ToSlash(rel)) {
		return fmt.Errorf("%s %s 跳出了包", at, cmd)
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode()&0o111 == 0 {
		return fmt.Errorf("%s %s 不是可执行文件", at, cmd)
	}
	return nil
}

// conflicts says which contribution of m another plugin or the hub has already. Called with r.mu held.
func (r *Registry) conflicts(m *Manifest) error {
	var errs []error
	for point, ids := range m.Contributions() {
		for _, id := range ids {
			if who, ok := r.reserved[point][id]; ok {
				errs = append(errs, fmt.Errorf("%s 里的 %q 已经是 %s 的", point, id, who))
			}
			for _, other := range r.extensions {
				if other.Record.ID != m.ID && slices.Contains(other.Full.Items(point), id) {
					errs = append(errs, fmt.Errorf("%s 里的 %q 已经是扩展 %s 的", point, id, other.Record.ID))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// expire drops previews past their time.
func (r *Registry) expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, pv := range r.previews {
		if r.now().After(pv.ExpiresAt) {
			if pv.staging != "" {
				_ = os.RemoveAll(pv.staging)
			}
			delete(r.previews, id)
		}
	}
}

// take is the preview previewID, removed from the waiting ones.
// canEnable asks CanEnable, when the hub set it.
func (r *Registry) canEnable(ctx context.Context, source string, m *Manifest) error {
	if r.CanEnable == nil {
		return nil
	}
	return r.CanEnable(ctx, source, m)
}

// peek is a preview, left in place (a refusal before installing does not use it up).
func (r *Registry) peek(previewID string) *Preview {
	r.expire()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.previews[previewID]
}

func (r *Registry) take(previewID string) (*Preview, error) {
	r.expire()
	r.mu.Lock()
	defer r.mu.Unlock()
	pv := r.previews[previewID]
	if pv == nil {
		return nil, errors.New("预览过期了，重新预览一次")
	}
	delete(r.previews, previewID)
	return pv, nil
}

// Install installs a previewed package, digest being the one the user was shown. It is on at once, so CanEnable is
// asked first (a refusal keeps the preview).
func (r *Registry) Install(ctx context.Context, previewID, digest, actor string) (*Extension, error) {
	if pv := r.peek(previewID); pv != nil && !pv.Upgrade {
		if err := r.canEnable(ctx, SourceInstalled, pv.Manifest); err != nil {
			return nil, err
		}
	}
	pv, err := r.take(previewID)
	if err != nil {
		return nil, err
	}
	defer r.dropStaging(pv)
	if pv.Upgrade {
		return nil, errors.New("这是升级的预览")
	}
	if digest != pv.Digest {
		return nil, errors.New("权限摘要对不上：装的不是你看过的那一份")
	}
	m := pv.Manifest
	now := r.now()
	row := Record{ID: m.ID, Source: SourceInstalled, Origin: pv.source.String(), Version: m.Version, Granted: m.Permissions,
		GrantedDigest: pv.Digest, Enabled: true, Config: json.RawMessage(`{}`), InstalledAt: now, UpdatedAt: now}
	row.Manifest, _ = json.Marshal(m)
	if pv.source.Kind == "remote" {
		row.Remote = &Remote{RPCSocket: pv.source.RPCSocket, HTTPSocket: pv.source.HTTPSocket}
	} else if err := r.place(pv, m); err != nil {
		return nil, err
	}
	r.mu.Lock()
	builtin := r.extensions[m.ID]
	if builtin != nil && !r.yielding(builtin) {
		r.mu.Unlock()
		return nil, fmt.Errorf("已经有一个叫 %s 的扩展", m.ID)
	}
	if err := r.conflicts(m); err != nil {
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()
	if err := r.store.PutExtension(ctx, row); err != nil {
		return nil, err
	}
	p := r.fromRow(row)
	r.mu.Lock()
	if builtin != nil {
		r.yielded[m.ID] = builtin
	}
	r.extensions[m.ID] = p
	r.mu.Unlock()
	r.audit(ctx, m.ID, "install", actor, map[string]any{"version": m.Version, "origin": row.Origin, "permissions": m.Permissions})
	r.changed(Change{ID: m.ID, Kind: "installed", Old: builtin}) // the hub stops what ran of id first (the built-in)
	return p, nil
}

// place moves a staged package to installed/<id>/<version>.
func (r *Registry) place(pv *Preview, m *Manifest) error {
	to := r.installedDir(m.ID, m.Version)
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(pv.root, to)
}

func (r *Registry) dropStaging(pv *Preview) {
	if pv.staging != "" {
		_ = os.RemoveAll(pv.staging)
	}
}

// Upgrade installs a previewed newer version of id. The digest is needed when it asks for more than was granted. A
// plugin that is on stays on, so CanEnable is asked about the new version first (a refusal keeps the preview).
func (r *Registry) Upgrade(ctx context.Context, id, previewID, digest, actor string) (*Extension, error) {
	if pv, cur := r.peek(previewID), r.Get(id); pv != nil && pv.Upgrade && pv.Manifest.ID == id && cur != nil && cur.Record.Enabled {
		if err := r.canEnable(ctx, cur.Record.Source, pv.Manifest); err != nil {
			return nil, err
		}
	}
	pv, err := r.take(previewID)
	if err != nil {
		return nil, err
	}
	defer r.dropStaging(pv)
	m := pv.Manifest
	if !pv.Upgrade || m.ID != id {
		return nil, fmt.Errorf("这不是 %s 的升级预览", id)
	}
	if (pv.MorePermissions || digest != "") && digest != pv.Digest {
		return nil, errors.New("新版本要的权限变多了：确认新的权限后再升级")
	}
	old := r.Get(id)
	if old == nil || old.Record.Version != pv.From {
		return nil, errors.New("扩展在预览之后变了，重新预览一次")
	}
	row := old.Record
	row.PreviousVersion, row.Version, row.UpdatedAt = row.Version, m.Version, r.now()
	row.Manifest, _ = json.Marshal(m)
	row.Granted, row.GrantedDigest = m.Permissions, pv.Digest
	row.Origin = pv.source.String()
	if pv.source.Kind == "remote" {
		row.Remote = &Remote{RPCSocket: pv.source.RPCSocket, HTTPSocket: pv.source.HTTPSocket}
	} else {
		row.Remote = nil
		if err := r.place(pv, m); err != nil {
			return nil, err
		}
	}
	if err := r.store.PutExtension(ctx, row); err != nil {
		return nil, err
	}
	p := r.fromRow(row)
	r.mu.Lock()
	r.extensions[id] = p
	r.mu.Unlock()
	r.prune(id, row.Version, row.PreviousVersion)
	r.audit(ctx, id, "upgrade", actor, map[string]any{"from": row.PreviousVersion, "to": row.Version, "permissions": m.Permissions})
	r.changed(Change{ID: id, Kind: "upgraded", Old: old})
	return p, nil
}

// prune deletes the package directories of id other than keep.
func (r *Registry) prune(id string, keep ...string) {
	entries, _ := os.ReadDir(filepath.Join(r.dir, "installed", id))
	for _, e := range entries {
		if !slices.Contains(keep, e.Name()) {
			_ = os.RemoveAll(filepath.Join(r.dir, "installed", id, e.Name()))
		}
	}
}

// Rollback goes back to the version installed before the last upgrade.
func (r *Registry) Rollback(ctx context.Context, id, actor string) (*Extension, error) {
	old := r.Get(id)
	if old == nil || old.Record.Source != SourceInstalled {
		return nil, fmt.Errorf("没有装过扩展 %s", id)
	}
	prev := old.Record.PreviousVersion
	if prev == "" || old.Record.Remote != nil {
		return nil, errors.New("没有可以回到的上一个版本")
	}
	data, err := os.ReadFile(ManifestPath(r.installedDir(id, prev)))
	if err != nil {
		return nil, fmt.Errorf("上一个版本 %s 的包不在了", prev)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	row := old.Record
	row.Version, row.PreviousVersion, row.UpdatedAt = prev, "", r.now()
	row.Manifest, _ = json.Marshal(m)
	row.Granted, row.GrantedDigest = m.Permissions, Digest(m)
	if err := r.store.PutExtension(ctx, row); err != nil {
		return nil, err
	}
	p := r.fromRow(row)
	r.mu.Lock()
	r.extensions[id] = p
	r.mu.Unlock()
	r.prune(id, prev)
	r.audit(ctx, id, "rollback", actor, map[string]any{"from": old.Record.Version, "to": prev})
	r.changed(Change{ID: id, Kind: "rolled_back", Old: old})
	return p, nil
}

// Uninstall removes an installed plugin: its row and secrets, its packages and (unless keepData) its data.
func (r *Registry) Uninstall(ctx context.Context, id string, keepData bool, actor string) error {
	old := r.Get(id)
	if old == nil {
		return fmt.Errorf("没有扩展 %s", id)
	}
	if old.Record.Source == SourceDev {
		return r.RemoveDev(id)
	}
	if old.Record.Source != SourceInstalled {
		return fmt.Errorf("%s 是%s的扩展，只能停用", id, map[string]string{SourceBuiltin: "内置", SourceHubJSON: " hub.json 里"}[old.Record.Source])
	}
	if err := r.store.DeleteExtension(ctx, id); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.extensions, id)
	back := r.yielded[id]
	if back != nil { // the built-in it took the place of comes back
		delete(r.yielded, id)
		r.extensions[id] = back
	}
	r.mu.Unlock()
	_ = os.RemoveAll(filepath.Join(r.dir, "installed", id))
	if !keepData {
		_ = os.RemoveAll(r.DataDir(id))
	}
	r.audit(ctx, id, "uninstall", actor, map[string]any{"version": old.Record.Version, "keep_data": keepData})
	r.changed(Change{ID: id, Kind: "removed", Old: old})
	return nil
}

// editable is a plugin the user may switch and configure (not hub.json's).
func (r *Registry) editable(id string) (*Extension, error) {
	p := r.Get(id)
	if p == nil {
		return nil, fmt.Errorf("没有扩展 %s", id)
	}
	if p.ReadOnly() {
		return nil, fmt.Errorf("%s 写在 hub.json 里：去 hub.json 或管理界面改", id)
	}
	if p.Record.Source == SourceDev {
		return nil, fmt.Errorf("%s 是开发中的扩展：改它目录里的 extension.dev.json，再跑一次 personad extension dev", id)
	}
	return p, nil
}

// put keeps p's row (a built-in one gets its first row here).
func (r *Registry) put(ctx context.Context, row Record) error {
	if r.store == nil {
		return nil // no database: the choice lasts until the hub restarts
	}
	if row.InstalledAt.IsZero() {
		row.InstalledAt = r.now()
	}
	row.UpdatedAt = r.now()
	return r.store.PutExtension(ctx, row)
}

// SetEnabled switches a plugin on or off; on only when CanEnable lets it.
func (r *Registry) SetEnabled(ctx context.Context, id string, on bool, actor string) error {
	p, err := r.editable(id)
	if err != nil {
		return err
	}
	if on && !p.Record.Enabled {
		if err := r.canEnable(ctx, p.Record.Source, p.Manifest); err != nil {
			return err
		}
	}
	r.mu.Lock()
	row := p.Record
	row.Enabled = on
	r.mu.Unlock()
	if err := r.put(ctx, row); err != nil {
		return err
	}
	r.mu.Lock()
	p.Record.Enabled = on
	r.mu.Unlock()
	kind := map[bool]string{true: "enabled", false: "disabled"}[on]
	r.audit(ctx, id, kind, actor, nil)
	r.changed(Change{ID: id, Kind: kind})
	return nil
}

// SetConfig replaces a plugin's configuration, held to its schema first.
func (r *Registry) SetConfig(ctx context.Context, id string, raw json.RawMessage, actor string) error {
	p, err := r.editable(id)
	if err != nil {
		return err
	}
	if err := p.Schema.Check(raw); err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	compact, _ := json.Marshal(v)
	r.mu.Lock()
	row := p.Record
	row.Config = compact
	r.mu.Unlock()
	if err := r.put(ctx, row); err != nil {
		return err
	}
	r.mu.Lock()
	p.Record.Config = compact
	r.mu.Unlock()
	r.audit(ctx, id, "config", actor, nil) // not the values: they may be personal
	r.changed(Change{ID: id, Kind: "config"})
	return nil
}

// Config is what the plugin gets: its configuration with the schema's defaults.
func (r *Registry) Config(p *Extension) json.RawMessage {
	r.mu.Lock()
	raw := p.Record.Config
	r.mu.Unlock()
	return p.Schema.Effective(raw)
}

func (r *Registry) secretSpec(p *Extension, name string) error {
	if !slices.ContainsFunc(p.Manifest.Secrets, func(s SecretSpec) bool { return s.Name == name }) {
		return fmt.Errorf("%s 没有叫 %s 的密钥", p.Record.ID, name)
	}
	return nil
}

// SetSecret keeps one of a plugin's declared secrets, sealed. The value never goes to a log or back to anyone.
func (r *Registry) SetSecret(ctx context.Context, id, name, value, actor string) error {
	p, err := r.editable(id)
	if err != nil {
		return err
	}
	if err := r.secretSpec(p, name); err != nil {
		return err
	}
	if value == "" || len(value) > SecretMost {
		return fmt.Errorf("密钥要有内容，最多 %d KB", SecretMost>>10)
	}
	if r.store == nil || r.seal == nil {
		return errors.New("没有数据库：存不了密钥")
	}
	if err := r.ensureRow(ctx, p); err != nil {
		return err
	}
	sealed, err := r.seal.Seal(value)
	if err != nil {
		return err
	}
	if err := r.store.PutExtensionSecret(ctx, id, name, sealed); err != nil {
		return err
	}
	r.audit(ctx, id, "secret_set", actor, map[string]any{"name": name})
	r.changed(Change{ID: id, Kind: "secret"})
	return nil
}

// DeleteSecret forgets one of a plugin's secrets.
func (r *Registry) DeleteSecret(ctx context.Context, id, name, actor string) error {
	p, err := r.editable(id)
	if err != nil {
		return err
	}
	if err := r.secretSpec(p, name); err != nil {
		return err
	}
	if r.store == nil {
		return nil
	}
	if err := r.store.DeleteExtensionSecret(ctx, id, name); err != nil {
		return err
	}
	r.audit(ctx, id, "secret_delete", actor, map[string]any{"name": name})
	r.changed(Change{ID: id, Kind: "secret"})
	return nil
}

// ensureRow gives a built-in plugin its row before a secret refers to it.
func (r *Registry) ensureRow(ctx context.Context, p *Extension) error {
	if p.Record.Source != SourceBuiltin {
		return nil
	}
	rows, err := r.store.Extensions(ctx)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(rows, func(x Record) bool { return x.ID == p.Record.ID }) {
		return nil
	}
	r.mu.Lock()
	row := p.Record
	r.mu.Unlock()
	return r.put(ctx, row)
}

// Secrets are a plugin's secrets in plain text, for handing to it only.
func (r *Registry) Secrets(ctx context.Context, id string) (map[string]string, error) {
	r.mu.Lock()
	dev, isDev := r.devSecrets[id]
	r.mu.Unlock()
	if isDev {
		return maps.Clone(dev), nil
	}
	if r.store == nil || r.seal == nil {
		return nil, nil
	}
	sealed, err := r.store.ExtensionSecrets(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(sealed))
	for name, s := range sealed {
		plain, err := r.seal.Open(s)
		if err != nil {
			return nil, fmt.Errorf("%s 的密钥 %s 打不开（secret.key 换过？）", id, name)
		}
		out[name] = plain
	}
	return out, nil
}

// SecretsSet says which declared secrets have a value.
func (r *Registry) SecretsSet(ctx context.Context, p *Extension) map[string]bool {
	out := map[string]bool{}
	if p.Record.Source == SourceDev {
		r.mu.Lock()
		for _, s := range p.Manifest.Secrets {
			_, out[s.Name] = r.devSecrets[p.Record.ID][s.Name]
		}
		r.mu.Unlock()
		return out
	}
	if r.store == nil || p.ReadOnly() {
		return out
	}
	sealed, err := r.store.ExtensionSecrets(ctx, p.Record.ID)
	if err != nil {
		return out
	}
	for _, s := range p.Manifest.Secrets {
		_, out[s.Name] = sealed[s.Name]
	}
	return out
}

// Missing are what a plugin needs before it can run: required secrets without a value, a configuration its schema
// does not take (after an upgrade changed it). Empty: ready.
func (r *Registry) Missing(ctx context.Context, p *Extension) []string {
	var out []string
	if p.ReadOnly() {
		return nil
	}
	set := r.SecretsSet(ctx, p)
	for _, s := range p.Manifest.Secrets {
		if s.Required && !set[s.Name] {
			out = append(out, "密钥「"+s.Title+"」")
		}
	}
	r.mu.Lock()
	raw := p.Record.Config
	r.mu.Unlock()
	if err := p.Schema.Check(raw); err != nil {
		out = append(out, "配置（"+err.Error()+"）")
	}
	return out
}

// Audit records what the hub changed about extension id outside the registry (its sandbox).
func (r *Registry) Audit(ctx context.Context, id, action, actor string, detail any) {
	r.audit(ctx, id, action, actor, detail)
}

func (r *Registry) audit(ctx context.Context, id, action, actor string, detail any) {
	if r.store == nil {
		return
	}
	b, _ := json.Marshal(detail)
	if detail == nil {
		b = []byte(`{}`)
	}
	_ = r.store.AddExtensionAudit(ctx, id, action, actor, b)
}

func (r *Registry) changed(c Change) {
	if r.OnChange != nil {
		r.OnChange(c)
	}
}
