package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Permissions are what a plugin asks for (docs/plan-plugins.md「权限」). The vocabulary is closed: a manifest naming
// a permission this hub does not know does not install, so nobody is shown one the hub cannot explain.
type Permissions struct {
	// Network are the hosts it reaches ("api.example.com", "*.example.com"). Shown and recorded; the hub does not
	// enforce them yet.
	Network []string `json:"network,omitempty"`
	// HubAPI: it gets the hub's address and full token (PERSONA_HUB_URL, PERSONA_HUB_TOKEN).
	HubAPI bool `json:"hub_api,omitempty"`
	// LauncherKeys: it reads launchers' quota with their keys — never seeing them: the hub (or the machine with the
	// launcher) sends its requests, to the launcher's own gateway only (docs/plugins.md「额度」).
	LauncherKeys bool `json:"launcher_keys,omitempty"`
}

var hostPattern = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func (p Permissions) validate() error {
	var errs []error
	for _, h := range p.Network {
		if !hostPattern.MatchString(h) || len(h) > 253 {
			errs = append(errs, fmt.Errorf("permissions.network: %q is not a host name (lowercase, no scheme, port or path; *. allowed first)", h))
		}
	}
	return errors.Join(errs...)
}

// normalized is p with the hosts sorted and once each.
func (p Permissions) normalized() Permissions {
	out := p
	out.Network = slices.Clone(p.Network)
	slices.Sort(out.Network)
	out.Network = slices.Compact(out.Network)
	return out
}

// Covers says whether p grants everything q asks (an upgrade asking for no more needs no new consent).
func (p Permissions) Covers(q Permissions) bool {
	if q.HubAPI && !p.HubAPI || q.LauncherKeys && !p.LauncherKeys {
		return false
	}
	for _, h := range q.Network {
		if !slices.Contains(p.Network, h) {
			return false
		}
	}
	return true
}

// Digest is what the user agrees to when installing: the permissions and whether it runs code, as a sha256 of their
// canonical JSON. Install and upgrade carry the digest of the preview the user saw, so what installs is what they saw.
func Digest(m *Manifest) string {
	b, _ := json.Marshal(struct {
		RunsCode    bool        `json:"runs_code"`
		Permissions Permissions `json:"permissions"`
	}{m.Runtime != nil, m.Permissions.normalized()})
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
	runsCode     = PermissionLine{Key: "runs_code", Title: "在 hub 机器上运行程序", Detail: "以 hub 的系统用户运行，能读写这个用户能读写的文件；不在沙箱里"}
	launcherKeys = PermissionLine{Key: "launcher_keys", Title: "用启动器的 Key 查额度", Detail: "它看不到 Key：hub 或有这个启动器的电脑替它发请求，只发到启动器自己的网关"}
	hubAPI       = PermissionLine{Key: "hub_api", Title: "完整使用 hub", Detail: "拿到 hub 的地址和 token，能做 hub 主人能做的一切：读知识库、驱动各台电脑上的 Agent……"}
)

// Explain is every line the user is shown before installing m.
func Explain(m *Manifest) []PermissionLine {
	out := []PermissionLine{} // never null on the wire: nothing to agree to is an empty list
	if m.Runtime != nil {
		out = append(out, runsCode)
	}
	if len(m.Permissions.Network) > 0 {
		out = append(out, PermissionLine{Key: "network", Title: "访问网络",
			Detail: strings.Join(m.Permissions.normalized().Network, "、") + "（hub 暂不拦截别的地址）"})
	}
	if m.Permissions.HubAPI {
		out = append(out, hubAPI)
	}
	if m.Permissions.LauncherKeys {
		out = append(out, launcherKeys)
	}
	if len(m.Secrets) > 0 {
		names := make([]string, 0, len(m.Secrets))
		for _, s := range m.Secrets {
			names = append(names, s.Title)
		}
		out = append(out, PermissionLine{Key: "secrets", Title: "使用你填的密钥", Detail: strings.Join(names, "、")})
	}
	return out
}

// Added is what upgrading from old to m adds to what the plugin may do, in Explain's words: running a program when old
// ran none, the hosts and secrets old did not have, the hub's token or launchers' keys when old did not get them. Nil when nothing is added.
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
	if m.Permissions.HubAPI && !old.Permissions.HubAPI {
		out = append(out, hubAPI)
	}
	if m.Permissions.LauncherKeys && !old.Permissions.LauncherKeys {
		out = append(out, launcherKeys)
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
	return out
}
