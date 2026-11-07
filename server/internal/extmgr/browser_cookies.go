package extmgr

// Sign-ins in ego lite on a Mac (#1304, docs/plans/plan-ego-browser.md "Importing cookies"): the machine page's "Browser
// sign-ins" lists the sites with cookies there, imports cookies the user picked in a file on their phone or in the web
// app, and clears a site's or all. Cookies are credentials:
//   - only the user's own signed-in apps may do any of it (byOwnDevice): not the pairing token in hub.json the CLI and
//     the assistant's processes hold, not a watch, a machine or another hub; and no tool of the assistant's reaches it;
//   - the hub passes an import straight to the node and keeps nothing: not in the database, not on disk, not in a log
//     (the log says which machine, how many and which sites), so the machine must be online;
//   - the node writes them through the script's stdin (node/browser_cookies.go) and gives back no value.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// maxCookieBody bounds an import's body: a whole browser's jar is well under it.
const maxCookieBody = 8 << 20

// browserCookies: GET (list) and POST (import {"cookies": [...]}) /api/v1/machines/{mid}/browser/cookies, DELETE with
// ?site=a&site=b or ?all=1 (clear).
func (h *Manager) browserCookies(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !byOwnDevice(r.Context()) {
		writeErr(w, http.StatusForbidden, i18n.Err("hub-browser_cookies.err_forbidden"))
		return
	}
	if h.browserOf(m) == nil {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-browser_cookies.err_off"))
		return
	}
	if !m.isOnline() {
		writeErr(w, http.StatusConflict, i18n.Err("hub-browser_cookies.err_offline"))
		return
	}
	method, params := "browser.cookies.list", map[string]any{}
	what := ""
	switch r.Method {
	case http.MethodPost:
		var in struct {
			Cookies []proto.BrowserCookie `json:"cookies"`
		}
		if err := decodeJSONBody(w, r, &in, maxCookieBody); err != nil {
			writeBodyErr(w, err, i18n.Err("hub-browser_cookies.err_bad_cookies"))
			return
		}
		if len(in.Cookies) == 0 {
			writeErr(w, http.StatusBadRequest, i18n.Err("hub-browser_cookies.err_no_cookies"))
			return
		}
		method, params["cookies"] = "browser.cookies.import", in.Cookies
		what = fmt.Sprintf("import %d for %s", len(in.Cookies), cookieSites(in.Cookies))
	case http.MethodDelete:
		q := r.URL.Query()
		all := q.Get("all") == "1"
		sites := q["site"]
		if !all && len(sites) == 0 {
			writeErr(w, http.StatusBadRequest, i18n.Err("hub-browser_cookies.err_which_sites"))
			return
		}
		method, params["sites"], params["all"] = "browser.cookies.clear", sites, all
		what = "clear " + strings.Join(sites, ",")
		if all {
			what = "clear all"
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute+15*time.Second)
	defer cancel()
	var out proto.BrowserCookies
	if err := m.callBuiltin(ctx, method, params, &out); err != nil {
		if strings.Contains(err.Error(), "unknown method") {
			err = i18n.Err("hub-browser_cookies.err_outdated")
		}
		writeErr(w, http.StatusConflict, err)
		return
	}
	if what != "" { // an audit line: who did what where, never a value
		h.logf("[%s] browser cookies %s by %s: imported %d, skipped %d, removed %d", m.cfg.ID, what, principalOf(r.Context()).Name, out.Imported, out.Skipped, out.Removed)
	}
	if out.Sites == nil {
		out.Sites = []proto.BrowserCookieSite{}
	}
	writeJSON(w, out)
}

// cookieSites names the sites of an import for the log: at most 10, then how many more.
func cookieSites(cs []proto.BrowserCookie) string {
	seen := map[string]bool{}
	for _, c := range cs {
		seen[strings.TrimPrefix(strings.ToLower(c.Domain), ".")] = true
	}
	list := make([]string, 0, len(seen))
	for d := range seen {
		list = append(list, d)
	}
	sort.Strings(list)
	if len(list) > 10 {
		return strings.Join(list[:10], ",") + fmt.Sprintf(" +%d", len(list)-10)
	}
	return strings.Join(list, ",")
}
