package hub

// Per-app approval for the assistant operating a Mac (docs/computer-use.md「锁屏、防睡眠、按 App 批准」c, #1507): the
// first time it operates an app on a Mac (or reads its whole screen) the phone asks once, 允许这一次 / 一直允许 / 拒绝.
// Apps always allowed are kept by bundle id for that Mac in the hub's settings (computerAppsKey) and listed, and
// removable, on the machine's page. Some are never remembered and asked about every time (askedEveryTime), and so is
// a call whose apps cannot all be told (a script naming them by expression, a step with no app).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

const computerAppsKey = "computer.apps"

// wholeScreen stands for computer_state with no app: everything on the Mac's screen.
const wholeScreen = "整个屏幕"

// approvedApp is an app always allowed on a Mac.
type approvedApp struct {
	BundleID string    `json:"bundle_id"`
	Name     string    `json:"name"`
	At       time.Time `json:"at"`
}

var computerAppsMu sync.Mutex

// askedEveryTime: terminals, System Settings, Keychain Access, password managers and Persona, by bundle id or by the
// name macOS shows (English or Chinese), case aside.
func askedEveryTime(a approvedApp) bool {
	if askedEveryTimeIDs[strings.ToLower(a.BundleID)] {
		return true
	}
	return slices.Contains(askedEveryTimeNames, strings.ToLower(strings.TrimSpace(a.Name)))
}

var askedEveryTimeIDs = map[string]bool{
	"com.apple.terminal": true, "com.googlecode.iterm2": true, "dev.warp.warp-stable": true, "com.mitchellh.ghostty": true,
	"io.alacritty": true, "net.kovidgoyal.kitty": true, "com.github.wez.wezterm": true,
	"com.apple.systempreferences": true, "com.apple.settings": true, "com.apple.keychainaccess": true,
	"com.apple.passwords": true, "com.1password.1password": true, "com.agilebits.onepassword7": true,
	"com.bitwarden.desktop": true, "com.lastpass.lastpass": true, "com.dashlane.dashlane": true,
	"org.keepassxc.keepassxc": true, "in.sinew.enpass-desktop": true,
}

var askedEveryTimeNames = []string{
	"terminal", "终端", "iterm", "iterm2", "warp", "ghostty", "alacritty", "kitty", "wezterm",
	"system settings", "系统设置", "system preferences", "系统偏好设置", "keychain access", "钥匙串访问",
	"passwords", "密码", "1password", "bitwarden", "lastpass", "dashlane", "keepassxc", "enpass",
	"persona", "persona link", "jarvis",
}

// approval: the plugin's 「第一次操作一个 App 时先问」, on unless turned off.
func (s cuaSettings) approval() bool { return s.AppApproval == nil || *s.AppApproval }

var getAppLiteral = regexp.MustCompile(`getApp\(\s*(?:"([^"\\]+)"|'([^'\\]+)')\s*\)`)

// callApps is what a computer tool call names: its apps by name (or wholeScreen), and whether that is all it may
// touch (false: a script naming its apps by expression or none, a step with no app of its own or the call's).
func callApps(name, app, script string, steps json.RawMessage) (apps []string, whole bool) {
	add := func(a string) {
		if a = strings.TrimSpace(a); a != "" && !slices.Contains(apps, a) {
			apps = append(apps, a)
		}
	}
	switch {
	case name == "computer_state":
		if app == "" {
			return []string{wholeScreen}, true
		}
		return []string{app}, true
	case script != "":
		for _, m := range getAppLiteral.FindAllStringSubmatch(script, -1) {
			add(m[1] + m[2])
		}
		return apps, len(apps) > 0 && strings.Count(script, "getApp(") == len(getAppLiteral.FindAllString(script, -1))
	}
	var list []map[string]json.RawMessage
	_ = json.Unmarshal(steps, &list)
	whole = len(list) > 0
	for _, s := range list {
		var named struct {
			App string `json:"app"`
		}
		found := ""
		for k, raw := range s {
			if k == "wait_for" || k == "expect" || k == "tool" {
				continue
			}
			if json.Unmarshal(raw, &named) == nil && named.App != "" {
				found = named.App
			}
		}
		if found == "" {
			found = app
		}
		if found == "" {
			whole = false
			continue
		}
		add(found)
	}
	return apps, whole
}

// appsOn is what m's Cua Driver knows of the apps named: bundle id and name, by the name given (case aside); a name
// it does not know stands for itself.
func (h *Hub) appsOn(ctx context.Context, m *Machine, names []string) []approvedApp {
	var known struct {
		Apps []struct {
			Name     string `json:"name"`
			BundleID string `json:"bundle_id"`
		} `json:"apps"`
	}
	if slices.ContainsFunc(names, func(n string) bool { return n != wholeScreen }) {
		if res, err := h.computerCall(ctx, m, "persona-apps", "list_apps", map[string]any{}); err == nil {
			_ = json.Unmarshal(res.Output, &known)
		}
	}
	out := make([]approvedApp, 0, len(names))
	for _, n := range names {
		a := approvedApp{BundleID: "name:" + strings.ToLower(n), Name: n}
		if n == wholeScreen {
			a.BundleID = "screen"
		}
		for _, k := range known.Apps {
			if k.BundleID != "" && (strings.EqualFold(k.Name, n) || strings.EqualFold(k.BundleID, n)) {
				a = approvedApp{BundleID: k.BundleID, Name: k.Name}
				break
			}
		}
		out = append(out, a)
	}
	return out
}

// approvedApps is the apps always allowed on each Mac.
func (h *Hub) approvedApps(ctx context.Context) map[string][]approvedApp {
	all := map[string][]approvedApp{}
	if h.store != nil {
		_, _ = h.store.Setting(ctx, computerAppsKey, &all)
	}
	return all
}

func (h *Hub) putApprovedApps(ctx context.Context, all map[string][]approvedApp) error {
	if h.store == nil {
		return errors.New("no database")
	}
	return h.store.PutSetting(ctx, computerAppsKey, all)
}

// approveApps asks on the phone about the apps this call names that m has not always allowed (or every time: those
// never remembered, a call whose apps are not all known). asked: a card was shown; ok: go ahead.
func (h *Hub) approveApps(ctx context.Context, m *Machine, tool string, names []string, whole bool, args json.RawMessage) (asked, ok bool) {
	if !h.cua().approval() {
		return false, true
	}
	apps := h.appsOn(ctx, m, names)
	kept := h.approvedApps(ctx)[m.cfg.ID]
	var need []approvedApp
	remember := whole
	for _, a := range apps {
		switch {
		case askedEveryTime(a):
			need, remember = append(need, a), false
		case !slices.ContainsFunc(kept, func(k approvedApp) bool { return k.BundleID == a.BundleID }):
			need = append(need, a)
		}
	}
	if whole && len(need) == 0 {
		return false, true
	}
	l := h.userLang()
	shown := make([]string, 0, len(need))
	for _, a := range need {
		name := a.Name
		if a.BundleID == "screen" {
			name = i18n.T(l, "hub-computer_apps.whole_screen")
		}
		shown = append(shown, i18n.T(l, "hub-computer_apps.app", i18n.Args{"name": name}))
	}
	appList := strings.Join(shown, i18n.T(l, "hub-computer_apps.list_sep"))
	summary := i18n.T(l, "hub-computer_apps.ask_apps", i18n.Args{"machine": m.cfg.Name, "apps": appList})
	if len(need) == 0 {
		summary = i18n.T(l, "hub-computer_apps.ask_run", i18n.Args{"machine": m.cfg.Name})
	}
	if !whole {
		summary = i18n.T(l, "hub-computer_apps.ask_unknown", i18n.Args{"summary": summary})
	}
	ix := proto.Interaction{
		ID: "ix_" + randomHex(6), MachineID: m.cfg.ID, Kind: proto.IxPermission, ToolName: tool, Summary: summary,
		ToolInput: args, SessionTitle: "Persona", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(15 * time.Minute).UTC(),
	}
	if remember {
		ix.AlwaysLabel = i18n.T(l, "hub-computer_apps.always", i18n.Args{"apps": appList})
	}
	r, _ := h.waitInteraction(ctx, ix)
	ok = r.Allow != nil && *r.Allow
	if ok && r.Always && remember {
		computerAppsMu.Lock()
		all := h.approvedApps(ctx)
		for _, a := range need {
			a.At = time.Now().UTC()
			all[m.cfg.ID] = append(all[m.cfg.ID], a)
		}
		if err := h.putApprovedApps(ctx, all); err != nil {
			h.logf("[%s] keeping the apps allowed: %v", m.cfg.ID, err)
		}
		computerAppsMu.Unlock()
		h.logf("[%s] %d app(s) always allowed for the assistant", m.cfg.ID, len(need))
	}
	return true, ok
}

// getComputerApps: GET /api/v1/machines/{mid}/computer/apps, the apps always allowed on this Mac.
func (h *Hub) getComputerApps(w http.ResponseWriter, r *http.Request, m *Machine) {
	apps := h.approvedApps(r.Context())[m.cfg.ID]
	if apps == nil {
		apps = []approvedApp{}
	}
	writeJSON(w, map[string]any{"apps": apps})
}

// deleteComputerApp: DELETE /api/v1/machines/{mid}/computer/apps/{bundle}, asked again next time.
func (h *Hub) deleteComputerApp(w http.ResponseWriter, r *http.Request, m *Machine) {
	id := r.PathValue("bundle")
	computerAppsMu.Lock()
	all := h.approvedApps(r.Context())
	all[m.cfg.ID] = slices.DeleteFunc(all[m.cfg.ID], func(a approvedApp) bool { return a.BundleID == id })
	err := h.putApprovedApps(r.Context(), all)
	computerAppsMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h.getComputerApps(w, r, m)
}
