package node

// Sign-ins in ego lite's default profile (#1304, docs/plans/plan-ego-browser.md "Importing cookies"): the sites that have
// cookies there (never a value), importing cookies the user picked on their phone or in the web app, and clearing a
// site's or all of them. Cookies are credentials: they reach ego through the script's stdin (never a command line or
// a file), and nothing here logs or returns a value. The work happens in a task space of Persona's own, through CDP's
// Network domain on its page (Storage.* is not open to task-space scripts): what a page can set, read and delete is
// the whole profile's.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/nwverse/persona/server/internal/proto"
)

const (
	cookieSpace = "persona cookies" // the task space; closed again at the end of each call
	// maxImport bounds one import: a browser's whole jar is a few thousand.
	maxImport = 20000
)

// cookieMark starts the line the script prints its result on.
const cookieMark = "@@persona-cookies@@"

// cookiesUnreadable is what the user is told when ego's side of a call failed.
const cookiesUnreadable = "读不到这台 Mac 上 ego 的登录，点「再试一次」；还不行就在那台 Mac 上重新打开 ego lite"

// cookieLog is where a failed script's error goes (the node's log; tests replace it).
var cookieLog = log.Printf

// cdpCookie is a cookie as CDP's Network.setCookies takes it.
type cdpCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires,omitempty"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	SameSite string  `json:"sameSite,omitempty"`
}

// toCDP turns the cookies into CDP's form, skipping what is expired or malformed. A host-only cookie's domain has no
// leading dot (CDP then keeps it to that host); a domain cookie's has one.
func toCDP(in []proto.BrowserCookie, now time.Time) (out []cdpCookie, skipped int) {
	for _, c := range in {
		d := strings.ToLower(strings.TrimSpace(c.Domain))
		host := strings.TrimPrefix(d, ".")
		if c.Name == "" || host == "" || strings.ContainsAny(host, "/ :") || len(c.Value) > 8192 || (c.Expires > 0 && c.Expires < float64(now.Unix())) {
			skipped++
			continue
		}
		if c.HostOnly {
			d = host
		} else {
			d = "." + host
		}
		path := c.Path
		if path == "" || !strings.HasPrefix(path, "/") {
			path = "/"
		}
		same := ""
		switch strings.ToLower(c.SameSite) {
		case "strict":
			same = "Strict"
		case "lax":
			same = "Lax"
		case "none", "no_restriction":
			same = "None"
		}
		secure := c.Secure || same == "None" // Chromium refuses SameSite=None without Secure
		out = append(out, cdpCookie{Name: c.Name, Value: c.Value, Domain: d, Path: path, Expires: c.Expires, Secure: secure, HTTPOnly: c.HTTPOnly, SameSite: same})
	}
	return out, skipped
}

// siteOf is the site a cookie domain belongs to, as the user knows it: example.com for .www.example.com.
func siteOf(domain string) string {
	host := strings.TrimPrefix(strings.ToLower(domain), ".")
	if s, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return s
	}
	return host
}

// rawCookie is what CDP's getAllCookies gives, only the fields the summary and the clear need.
type rawCookie struct {
	Name    string  `json:"name"`
	Domain  string  `json:"domain"`
	Path    string  `json:"path"`
	Expires float64 `json:"expires"`
	Session bool    `json:"session"`
}

// sitesOf sums cookies up by site, the sites by name.
func sitesOf(cs []rawCookie) []proto.BrowserCookieSite {
	by := map[string]*proto.BrowserCookieSite{}
	for _, c := range cs {
		s := siteOf(c.Domain)
		if s == "" {
			continue
		}
		e := by[s]
		if e == nil {
			e = &proto.BrowserCookieSite{Domain: s}
			by[s] = e
		}
		e.Count++
		if !c.Session && c.Expires > 0 {
			if t := time.Unix(int64(c.Expires), 0).UTC(); t.After(e.Expires) {
				e.Expires = t
			}
		}
	}
	out := make([]proto.BrowserCookieSite, 0, len(by))
	for _, e := range by {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

// cookieScript is the script for one call: optionally set cookies, optionally delete the cookies of some sites (or
// all), then list what is there, and close the task space. Only names, domains, paths and times come back. Nothing it
// is given is null (a nil slice marshals as null: the list's first line read null.length, #1926), and what ego gives
// back may be: no cookies at all, a cookie without a domain or path.
func cookieScript(set []cdpCookie, clear []string, clearAll bool) string {
	if set == nil {
		set = []cdpCookie{}
	}
	if clear == nil {
		clear = []string{}
	}
	setJSON, _ := json.Marshal(set)
	clearJSON, _ := json.Marshal(clear)
	var b strings.Builder
	b.WriteString("const task = await taskSpace(" + jsString(cookieSpace) + ");\n")
	b.WriteString("const page = task.page(\"p1\");\n")
	b.WriteString("const __r = {};\n")
	b.WriteString("const all = async () => { const r = await page.cdp(\"Network.getAllCookies\", {}); return ((r && r.cookies) || []).filter((c) => c && typeof c.domain === \"string\" && c.domain); };\n")
	b.WriteString("try {\n")
	b.WriteString("  const set = " + string(setJSON) + " || [];\n")
	b.WriteString("  for (let i = 0; i < set.length; i += 200) await page.cdp(\"Network.setCookies\", { cookies: set.slice(i, i + 200) });\n")
	b.WriteString("  __r.set = set.length;\n")
	b.WriteString("  const clear = " + string(clearJSON) + " || [], everything = " + fmt.Sprint(clearAll) + ";\n")
	b.WriteString("  if (everything || clear.length) {\n")
	b.WriteString("    const site = (d) => { d = d.replace(/^\\./, \"\").toLowerCase(); return clear.some((s) => d === s || d.endsWith(\".\" + s)); };\n")
	b.WriteString("    let n = 0;\n")
	b.WriteString("    for (const c of await all()) if (everything || site(c.domain)) { await page.cdp(\"Network.deleteCookies\", { name: c.name || \"\", domain: c.domain, path: c.path || \"/\" }); n++; }\n")
	b.WriteString("    __r.removed = n;\n")
	b.WriteString("  }\n")
	b.WriteString("  __r.cookies = (await all()).map((c) => ({ name: c.name || \"\", domain: c.domain, path: c.path || \"/\", expires: typeof c.expires === \"number\" ? c.expires : 0, session: !!c.session }));\n")
	b.WriteString("} catch (e) { __r.error = String(e && e.message || e); __r.code = e && e.code; }\n")
	b.WriteString("try { await task.finish({ keep: [] }); } catch (e) {}\n")
	b.WriteString("console.log(" + jsString(cookieMark) + " + JSON.stringify(__r));\n")
	return b.String()
}

// runEgo runs a script in ego-browser's embedded node, the script on stdin; the line after mark, parsed.
var runEgo = func(ctx context.Context, script, mark string) (map[string]json.RawMessage, error) {
	bin := egoBin()
	if bin == "" {
		return nil, errors.New("ego-browser 不在：先装 ego lite 并做完首次设置")
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "nodejs")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	text := out.String()
	i := strings.LastIndex(text, mark)
	if i < 0 {
		if cctx.Err() != nil {
			return nil, errors.New("ego-browser 超时")
		}
		// what ego printed may echo the script: never pass it on, it may hold cookie values
		if runErr != nil {
			return nil, fmt.Errorf("ego-browser 没有运行完：%v", runErr)
		}
		return nil, errors.New("ego-browser 没有给出结果")
	}
	line := text[i+len(mark):]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	var r map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return nil, errors.New("ego-browser 的结果读不懂")
	}
	return r, nil
}

// cookiesCall runs one browser.cookies.* call: import (set), clear (sites or all), or just list.
func cookiesCall(ctx context.Context, set []proto.BrowserCookie, clear []string, clearAll bool) (*proto.BrowserCookies, error) {
	if len(set) > maxImport {
		return nil, fmt.Errorf("一次最多导入 %d 条 cookie", maxImport)
	}
	cdp, skipped := toCDP(set, time.Now())
	if len(set) > 0 && len(cdp) == 0 {
		return nil, errors.New("没有能导入的 cookie：都已过期或格式不对")
	}
	sites := make([]string, 0, len(clear))
	for _, s := range clear {
		if s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "."); s != "" && !strings.ContainsAny(s, "/ :") {
			sites = append(sites, s)
		}
	}
	r, err := runEgo(ctx, cookieScript(cdp, sites, clearAll), cookieMark)
	if err != nil {
		return nil, err
	}
	if raw, ok := r["error"]; ok {
		var msg, code string
		_ = json.Unmarshal(raw, &msg)
		_ = json.Unmarshal(r["code"], &code)
		if code == "EGO_TASK_SPACE_USER_IN_CONTROL" || strings.Contains(msg, "USER_IN_CONTROL") {
			return nil, errors.New("你正在 ego 里接管 Persona 的任务空间：先交还，再导入")
		}
		// What failed in the script is ours to look at, not the user's: a JavaScript error said nothing they could act
		// on (#1926). The log has it; no cookie's name or value is in such a message.
		cookieLog("browser cookies: ego script failed: %s", oneLine(msg, 2))
		return nil, errors.New(cookiesUnreadable)
	}
	var cs []rawCookie
	_ = json.Unmarshal(r["cookies"], &cs)
	out := &proto.BrowserCookies{Sites: sitesOf(cs), Skipped: skipped}
	if len(cdp) > 0 {
		out.Imported = len(cdp)
	}
	_ = json.Unmarshal(r["removed"], &out.Removed)
	return out, nil
}

// browserCookies answers browser.cookies.list / .import / .clear.
func browserCookies(ctx context.Context, method string, params json.RawMessage) (*proto.BrowserCookies, error) {
	var p struct {
		Cookies []proto.BrowserCookie `json:"cookies"`
		Sites   []string              `json:"sites"`
		All     bool                  `json:"all"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	switch method {
	case "browser.cookies.list":
		return cookiesCall(ctx, nil, nil, false)
	case "browser.cookies.import":
		if len(p.Cookies) == 0 {
			return nil, errors.New("没有要导入的 cookie")
		}
		return cookiesCall(ctx, p.Cookies, nil, false)
	case "browser.cookies.clear":
		if !p.All && len(p.Sites) == 0 {
			return nil, errors.New("清除哪些网站？")
		}
		return cookiesCall(ctx, nil, p.Sites, p.All)
	}
	return nil, fmt.Errorf("unknown method %s", method)
}
