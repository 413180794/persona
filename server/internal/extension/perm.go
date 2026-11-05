package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/channel"
)

// Permissions are what a plugin asks for (docs/plan-extensions.md「权限」). The vocabulary is closed: a manifest naming
// a permission this hub does not know does not install, so nobody is shown one the hub cannot explain.
type Permissions struct {
	// Network are the hosts it reaches ("api.example.com", "*.example.com"). Shown and recorded; the hub does not
	// enforce them yet.
	Network []string `json:"network,omitempty"`
	// HubAPI: it gets the hub's address and full token (PERSONA_HUB_URL, PERSONA_HUB_TOKEN). The old way: a manifest
	// naming Hub scopes gets a token of those only, whatever HubAPI says.
	HubAPI bool `json:"hub_api,omitempty"`
	// Hub are the parts of the hub's API it calls (HubScopes): it gets a token that opens those routes and no other.
	Hub []string `json:"hub,omitempty"`
	// LauncherKeys: it reads launchers' quota with their keys — never seeing them: the hub (or the machine with the
	// launcher) sends its requests, to the launcher's own gateway only (docs/extensions.md「额度」).
	LauncherKeys bool `json:"launcher_keys,omitempty"`
	// GhLogin: it asks the hub for the GitHub token of a computer's gh login (the one the user picks for the GitHub
	// connector), held in its memory only (docs/extensions.md「权限」). For the GitHub connector moved out of the hub.
	GhLogin bool `json:"gh_login,omitempty"`
}

var hostPattern = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func (p Permissions) validate() error {
	var errs []error
	for _, h := range p.Network {
		if !hostPattern.MatchString(h) || len(h) > 253 {
			errs = append(errs, fmt.Errorf("permissions.network: %q is not a host name (lowercase, no scheme, port or path; *. allowed first)", h))
		}
	}
	for _, s := range p.Hub {
		if _, ok := ScopeTitle(s); !ok {
			errs = append(errs, fmt.Errorf("permissions.hub: %q is not a scope of the hub's API (%s, %s<extension id>)", s,
				strings.Join(HubScopes, ", "), ScopeCallPrefix))
		}
	}
	return errors.Join(errs...)
}

// normalized is p with the hosts and scopes sorted and once each.
func (p Permissions) normalized() Permissions {
	out := p
	out.Network = slices.Compact(slices.Sorted(slices.Values(p.Network)))
	out.Hub = slices.Compact(slices.Sorted(slices.Values(p.Hub)))
	return out
}

// FullHub says whether it gets the hub's full token: hub_api, the old way, without scopes.
func (p Permissions) FullHub() bool { return p.HubAPI && len(p.Hub) == 0 }

// Covers says whether p grants everything q asks (an upgrade asking for no more needs no new consent). The full token
// covers every scope; scopes never cover the full token.
func (p Permissions) Covers(q Permissions) bool {
	if q.FullHub() && !p.FullHub() || q.LauncherKeys && !p.LauncherKeys || q.GhLogin && !p.GhLogin {
		return false
	}
	for _, s := range q.Hub {
		if !p.FullHub() && !slices.Contains(p.Hub, s) {
			return false
		}
	}
	for _, h := range q.Network {
		if !slices.Contains(p.Network, h) {
			return false
		}
	}
	return true
}

// nodeConsent is what the user agrees to of a program on computers: the systems it runs on, what it asks for there,
// and which secrets go to the computers.
type nodeConsent struct {
	Systems     []string        `json:"systems"`
	Permissions NodePermissions `json:"permissions"`
	Secrets     []string        `json:"secrets,omitempty"`
}

// secretNames are the secrets its computers get (none without a program).
func (r *NodeRuntime) secretNames() []string {
	if r == nil {
		return nil
	}
	return r.Secrets
}

func (r *NodeRuntime) consent() *nodeConsent {
	if r == nil {
		return nil
	}
	c := &nodeConsent{Systems: r.Systems(), Permissions: r.Permissions, Secrets: slices.Clone(r.Secrets)}
	c.Permissions.Network = slices.Compact(slices.Sorted(slices.Values(r.Permissions.Network)))
	slices.Sort(c.Secrets)
	return c
}

// Digest is what the user agrees to when installing: the permissions and whether it runs code, as a sha256 of their
// canonical JSON. Install and upgrade carry the digest of the preview the user saw, so what installs is what they saw.
// A plugin without a node program digests as it did before there were any (what installed plugins were granted holds).
func Digest(m *Manifest) string {
	b, _ := json.Marshal(struct {
		RunsCode    bool         `json:"runs_code"`
		Permissions Permissions  `json:"permissions"`
		Events      []string     `json:"events,omitempty"`
		Surfaces    []string     `json:"surfaces,omitempty"`
		Node        *nodeConsent `json:"node,omitempty"`
	}{m.Runtime != nil, m.Permissions.normalized(), m.Topics(), m.Surfaces(), m.NodeProgram().consent()})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// PermissionLine is one line of what a plugin may do, as the install screen shows it.
type PermissionLine struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

var (
	runsCode     = PermissionLine{Key: "runs_code", Title: "在 hub 机器上运行程序", Detail: "以 hub 的系统用户运行；默认在沙箱里，只能读写自己的包和数据目录（受信任市场来的、主人关了沙箱的，能读写这个用户能读写的文件）"}
	launcherKeys = PermissionLine{Key: "launcher_keys", Title: "用启动器的 Key 访问它的网关", Detail: "查额度、画启动器页上的面板和它自己的页面（比如侧边栏里的额度）；它看不到 Key：hub 或有这个启动器的电脑替它发请求，只发到启动器自己的网关"}
	ghLogin      = PermissionLine{Key: "gh_login", Title: "用你电脑上 gh 的 GitHub 登录", Detail: "从你选的那台电脑上读 gh 登录的 token，只放在它的内存里，用来访问 GitHub"}
	hubAPI       = PermissionLine{Key: "hub_api", Title: "完整权限（旧写法）", Detail: "拿到 hub 的完整 token，能做 hub 主人能做的一切：读知识库、驱动各台电脑上的 Agent……；新的扩展只要用得到的那几项（permissions.hub）"}
	nodeFiles    = PermissionLine{Key: "node_files", Title: "读写这些电脑上的文件", Detail: "不只是它自己的数据目录"}
	nodePrivacy  = PermissionLine{Key: "node_macos_privacy", Title: "请求 Mac 的系统权限", Detail: "完全磁盘访问、辅助功能这类，要你在那台 Mac 上同意"}
)

// systemNames are how the install screen names the systems a node program runs on.
var systemNames = map[string]string{"darwin": "Mac", "linux": "Linux 电脑"}

// nodeRunsCode is the line for a program that runs on the computers the user picks.
func nodeRunsCode(r *NodeRuntime) PermissionLine {
	var on []string
	for _, sys := range r.Systems() {
		on = append(on, systemNames[sys])
	}
	return PermissionLine{Key: "node_runs_code", Title: "在你选的电脑上运行程序",
		Detail: strings.Join(on, "、") + "；每台打开时会再问一次。以 node 的系统用户运行，能读写这个用户能读写的文件；不在沙箱里"}
}

// nodeLines are the lines of what r asks for on the computers, of all it asks (old nil) or what it asks more than old.
func nodeLines(r, old *NodeRuntime) []PermissionLine {
	var out []PermissionLine
	if old == nil {
		out = append(out, nodeRunsCode(r))
	} else if slices.ContainsFunc(r.Systems(), func(sys string) bool { return !slices.Contains(old.Systems(), sys) }) {
		line := nodeRunsCode(r)
		line.Title = "在更多种电脑上运行程序"
		out = append(out, line)
	}
	var hosts []string
	for _, h := range r.consent().Permissions.Network {
		if old == nil || !slices.Contains(old.Permissions.Network, h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > 0 {
		out = append(out, PermissionLine{Key: "node_network", Title: "在这些电脑上访问网络", Detail: strings.Join(hosts, "、")})
	}
	if r.Permissions.Files && (old == nil || !old.Permissions.Files) {
		out = append(out, nodeFiles)
	}
	if r.Permissions.MacOSPrivacy && (old == nil || !old.Permissions.MacOSPrivacy) {
		out = append(out, nodePrivacy)
	}
	return out
}

// nodeSecrets are the titles of the secrets m sends to the computers that old (nil: none) did not.
func nodeSecrets(m, old *Manifest) []string {
	var out []string
	for _, name := range m.NodeProgram().secretNames() {
		if old.NodeProgram() != nil && slices.Contains(old.Node.Secrets, name) {
			continue
		}
		for _, s := range m.Secrets {
			if s.Name == name {
				out = append(out, s.Title)
			}
		}
	}
	return out
}

// NodeAdds says whether m's program on computers asks for more than old's: a first one, more systems, hosts,
// permissions or secrets sent to the computers.
func NodeAdds(old, m *Manifest) bool {
	return m.NodeProgram() != nil && (len(nodeLines(m.Node, old.NodeProgram())) > 0 || len(nodeSecrets(m, old)) > 0)
}

// Explain is every line the user is shown before installing m.
func Explain(m *Manifest) []PermissionLine {
	out := []PermissionLine{} // never null on the wire: nothing to agree to is an empty list
	if m.Runtime != nil {
		out = append(out, runsCode)
	}
	if len(m.Permissions.Network) > 0 {
		out = append(out, PermissionLine{Key: "network", Title: "访问网络",
			Detail: strings.Join(m.Permissions.normalized().Network, "、") + "（在沙箱里时只能连这些）"})
	}
	if m.Permissions.FullHub() {
		out = append(out, hubAPI)
	} else if hub := m.Permissions.normalized().Hub; len(hub) > 0 {
		out = append(out, hubLine("使用 hub 的这些功能", hub))
	}
	if m.Permissions.LauncherKeys {
		out = append(out, launcherKeys)
	}
	if m.Permissions.GhLogin {
		out = append(out, ghLogin)
	}
	if to := m.ContributesTo(); len(to) > 0 {
		out = append(out, PermissionLine{Key: "contributes_to", Title: "给别的扩展提供内容",
			Detail: "它在清单里写的贡献交给这些扩展：" + strings.Join(to, "、")})
	}
	if chans := m.Channels(); len(chans) > 0 {
		var names []string
		for _, c := range chans {
			names = append(names, c.Name)
		}
		out = append(out, PermissionLine{Key: "channel", Title: "作为和 Persona 对话的渠道",
			Detail: "把你在" + strings.Join(names, "、") + "里发给它的消息交给 Persona，再把回答和 Agent 的提问发回给你"})
	}
	if topics := m.Topics(); len(topics) > 0 {
		var what []string
		for _, t := range topics {
			what = append(what, EventTopics[t])
		}
		out = append(out, PermissionLine{Key: "events", Title: "收到 hub 的事件", Detail: strings.Join(what, "；")})
	}
	if surfaces := m.Surfaces(); len(surfaces) > 0 {
		title := "在 App 里显示自己的界面"
		if slices.Contains(surfaces, "capsule_card") {
			title = "在 App 内外显示自己的界面"
		}
		out = append(out, PermissionLine{Key: "ui_surfaces", Title: title, Detail: surfacesDetail(m, surfaces)})
	}
	if len(m.Secrets) > 0 {
		names := make([]string, 0, len(m.Secrets))
		for _, s := range m.Secrets {
			names = append(names, s.Title)
		}
		out = append(out, PermissionLine{Key: "secrets", Title: "使用你填的密钥", Detail: strings.Join(names, "、")})
	}
	if m.NodeProgram() != nil {
		out = append(out, nodeLines(m.Node, nil)...)
		if names := nodeSecrets(m, nil); len(names) > 0 {
			out = append(out, PermissionLine{Key: "node_secrets", Title: "把密钥发给这些电脑", Detail: strings.Join(names, "、")})
		}
	}
	return out
}

// Added is what upgrading from old to m adds to what the plugin may do, in Explain's words: running a program when old
// ran none, the hosts and secrets old did not have, the hub's token or launchers' keys when old did not get them, the
// channels and the extensions it contributes to that old did not have. Nil when nothing is added.
func Added(old, m *Manifest) []PermissionLine {
	var out []PermissionLine
	if m.Runtime != nil && old.Runtime == nil {
		out = append(out, runsCode)
	}
	var hosts []string
	for _, h := range m.Permissions.normalized().Network {
		if !slices.Contains(old.Permissions.Network, h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > 0 {
		out = append(out, PermissionLine{Key: "network", Title: "访问更多网络", Detail: strings.Join(hosts, "、")})
	}
	if m.Permissions.FullHub() && !old.Permissions.FullHub() {
		out = append(out, hubAPI)
	} else if !m.Permissions.FullHub() && !old.Permissions.FullHub() {
		var more []string
		for _, s := range m.Permissions.normalized().Hub {
			if !slices.Contains(old.Permissions.Hub, s) {
				more = append(more, s)
			}
		}
		if len(more) > 0 {
			out = append(out, hubLine("使用 hub 的更多功能", more))
		}
	}
	if m.Permissions.LauncherKeys && !old.Permissions.LauncherKeys {
		out = append(out, launcherKeys)
	}
	if m.Permissions.GhLogin && !old.Permissions.GhLogin {
		out = append(out, ghLogin)
	}
	var to []string
	for _, owner := range m.ContributesTo() {
		if !slices.Contains(old.ContributesTo(), owner) {
			to = append(to, owner)
		}
	}
	if len(to) > 0 {
		out = append(out, PermissionLine{Key: "contributes_to", Title: "给更多别的扩展提供内容",
			Detail: "它在清单里写的贡献交给这些扩展：" + strings.Join(to, "、")})
	}
	if chans := newChannels(old, m); len(chans) > 0 {
		var names []string
		for _, c := range chans {
			names = append(names, c.Name)
		}
		out = append(out, PermissionLine{Key: "channel", Title: "作为和 Persona 对话的更多渠道",
			Detail: "把你在" + strings.Join(names, "、") + "里发给它的消息交给 Persona，再把回答和 Agent 的提问发回给你"})
	}
	var topics []string
	for _, t := range m.Topics() {
		if !slices.Contains(old.Topics(), t) {
			topics = append(topics, EventTopics[t])
		}
	}
	if len(topics) > 0 {
		out = append(out, PermissionLine{Key: "events", Title: "收到更多 hub 的事件", Detail: strings.Join(topics, "；")})
	}
	var surfaces []string
	for _, sl := range m.Surfaces() {
		if !slices.Contains(old.Surfaces(), sl) {
			surfaces = append(surfaces, sl)
		}
	}
	if len(surfaces) > 0 {
		out = append(out, PermissionLine{Key: "ui_surfaces", Title: "在 App 的更多位置显示自己的界面", Detail: surfacesDetail(m, surfaces)})
	}
	var names []string
	for _, s := range m.Secrets {
		if !slices.ContainsFunc(old.Secrets, func(o SecretSpec) bool { return o.Name == s.Name }) {
			names = append(names, s.Title)
		}
	}
	if len(names) > 0 {
		out = append(out, PermissionLine{Key: "secrets", Title: "使用更多密钥", Detail: strings.Join(names, "、")})
	}
	if m.NodeProgram() != nil {
		out = append(out, nodeLines(m.Node, old.NodeProgram())...)
		if names := nodeSecrets(m, old); len(names) > 0 {
			out = append(out, PermissionLine{Key: "node_secrets", Title: "把更多密钥发给这些电脑", Detail: strings.Join(names, "、")})
		}
	}
	return out
}

// newChannels are m's channel contributions whose ids old does not contribute. A channel's messages reach the
// assistant as the owner's own (and can answer the owner's Agent asks), so a new one needs the user's consent.
func newChannels(old, m *Manifest) []channel.Item {
	var out []channel.Item
	for _, c := range m.Channels() {
		if !slices.ContainsFunc(old.Channels(), func(o channel.Item) bool { return o.ID == c.ID }) {
			out = append(out, c)
		}
	}
	return out
}

// surfacesDetail says where in the apps m draws, of surfaces: 主导航上的入口：Token 用量；总览上的卡片：…
func surfacesDetail(m *Manifest, surfaces []string) string {
	var parts []string
	for _, name := range surfaces {
		if name == SurfaceShortcuts {
			var titles []string
			for _, s := range m.Shortcuts() {
				titles = append(titles, s.Title)
			}
			parts = append(parts, "快捷指令和 Siri 里的动作："+strings.Join(titles, "、"))
			continue
		}
		sl, _ := Slot(name)
		var titles []string
		for _, u := range m.UIs() {
			if u.Placement == name {
				titles = append(titles, u.Title)
			}
		}
		parts = append(parts, sl.Shown+"："+strings.Join(titles, "、"))
	}
	return strings.Join(parts, "；")
}
