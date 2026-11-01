// Package quota reads how much a launcher's gateway key may still spend (docs/launchers.md「额度」): relay-history's
// self-usage report (GET /me/usage with the key) and FAKEMODEL's console quota (POST /auth/key-login with the key,
// then GET /api/quota on that read-only session). Both are folded into one shape the app draws, and nothing in it is
// a secret: no key, no account e-mail.
package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Report is one key's quota as the app shows it.
type Report struct {
	Gateway string `json:"gateway"`            // relay-history | fakemodel
	KeyName string `json:"key_name,omitempty"` // the key's billing name at the gateway (not the key)
	// Usage is the key's own use (relay-history: today / this week / this month).
	Usage []Usage `json:"usage,omitempty"`
	// Budget is a spending cap set on the key itself (relay-history), nil when none.
	Budget []Window `json:"budget,omitempty"`
	// Pools are what the key draws on: relay-history's accounts, FAKEMODEL's providers; each with its windows.
	Pools     []Pool    `json:"pools,omitempty"`
	Note      string    `json:"note,omitempty"` // e.g. the gateway does not share account quota with this key
	CheckedAt time.Time `json:"checked_at"`
}

type Usage struct {
	Label    string   `json:"label"` // 今日 / 本周 / 本月
	Requests int64    `json:"requests"`
	Tokens   int64    `json:"tokens"`
	CostUSD  *float64 `json:"cost_usd,omitempty"`
}

// Pool is an account or provider the key's requests go to.
type Pool struct {
	Name    string   `json:"name"`             // provider · plan, or the provider's name
	State   string   `json:"state,omitempty"`  // available | cooldown | exhausted | restricted | unavailable | ok | partial | error | unsupported
	Detail  string   `json:"detail,omitempty"` // why, or the state in words
	Windows []Window `json:"windows,omitempty"`
	// Models are FAKEMODEL's per-model windows that have an upstream limit.
	Models []ModelWindows `json:"models,omitempty"`
}

type ModelWindows struct {
	Model   string   `json:"model"`
	Windows []Window `json:"windows"`
}

// Window is one quota window: how much of it is used, of what, until when.
type Window struct {
	Label string `json:"label"` // 5 小时 / 每周 / 每月 / 累计预算…
	// UsedRatio is 0…1 when the limit is known.
	UsedRatio *float64   `json:"used_ratio,omitempty"`
	Used      *float64   `json:"used,omitempty"`
	Limit     *float64   `json:"limit,omitempty"`
	Unit      string     `json:"unit,omitempty"` // percent | tokens | requests | usd | credits
	ResetsAt  *time.Time `json:"resets_at,omitempty"`
	Exhausted bool       `json:"exhausted,omitempty"`
}

// Fetch reads the quota of key at the gateway behind baseURL (an Anthropic / OpenAI base URL, with or without /v1).
// It tries relay-history's self-usage first (it answers with service "relay-history"), then FAKEMODEL's key login for
// FAKEMODEL keys (fm_…). ErrUnsupported: the gateway is neither.
// tz is the user's time zone (IANA name): the gateway counts its daily windows in it and the report gives times in it.
// "" when it is not known: the gateway's own default, this machine's zone.
func Fetch(ctx context.Context, client *http.Client, baseURL, key, tz string) (Report, error) {
	root, err := root(baseURL)
	if err != nil {
		return Report{}, err
	}
	if key == "" {
		return Report{}, errors.New("这个启动器没有 Key")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.HasPrefix(key, "fm_") {
		return fetchFakemodel(ctx, client, root, key)
	}
	r, err := fetchrelay-history(ctx, client, root, key, tz)
	if errors.Is(err, errNotrelay-history) {
		return Report{}, ErrUnsupported
	}
	return r, err
}

// ErrUnsupported: the launcher's gateway has no quota this package can read.
var ErrUnsupported = errors.New("这个网关没有能查的额度（只支持 relay-history 和 FAKEMODEL）")

var errNotrelay-history = errors.New("not relay-history")

// root is the gateway's origin and path prefix without a trailing /v1.
func root(base string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("网关地址 %q 不对", base)
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

func endpoint(root *url.URL, path string, q url.Values) string {
	u := *root
	u.Path = strings.TrimRight(u.Path, "/") + path
	if q != nil {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func getJSON(ctx context.Context, client *http.Client, req *http.Request, out any) (int, error) {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error   any    `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &e)
		msg := e.Message
		if m, ok := e.Error.(map[string]any); ok {
			if s, ok := m["message"].(string); ok {
				msg = s
			}
		} else if s, ok := e.Error.(string); ok && msg == "" {
			msg = s
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d：%s", resp.StatusCode, msg)
	}
	return resp.StatusCode, json.Unmarshal(body, out)
}

// ---- relay-history ----

type asterBucket struct {
	Requests     int64    `json:"requests"`
	BilledTokens int64    `json:"billed_tokens"`
	CostUSD      *float64 `json:"cost_usd"`
}

type asterWindow struct {
	Kind      string     `json:"kind"`
	Scope     string     `json:"scope"`
	Model     string     `json:"scoped_model"`
	UsedRatio *float64   `json:"used_ratio"`
	Remaining *float64   `json:"remaining"`
	Limit     *float64   `json:"limit"`
	ResetsAt  *time.Time `json:"resets_at"`
}

type asterBudget struct {
	LimitUSD  float64          `json:"limit_usd"`
	SpentUSD  float64          `json:"spent_usd"`
	Exhausted bool             `json:"exhausted"`
	StartedAt *time.Time       `json:"started_at"`
	Weekly    *asterBudgetWeek `json:"weekly"`
}

type asterBudgetWeek struct {
	LimitUSD  float64    `json:"limit_usd"`
	SpentUSD  float64    `json:"spent_usd"`
	Exhausted bool       `json:"exhausted"`
	ResetsAt  *time.Time `json:"resets_at"`
}

type asterReport struct {
	Service            string       `json:"service"`
	APIKey             string       `json:"api_key"`
	Today              *asterBucket `json:"today"`
	Week               *asterBucket `json:"week"`
	Month              *asterBucket `json:"month"`
	Budget             *asterBudget `json:"budget"`
	AccountQuotaShared *bool        `json:"account_quota_shared"`
	Accounts           []struct {
		AccountID   string        `json:"account_id"`
		Provider    string        `json:"provider"`
		Plan        string        `json:"plan"`
		State       string        `json:"state"`
		AvailableAt *time.Time    `json:"available_at"`
		Current     bool          `json:"current"`
		Windows     []asterWindow `json:"windows"`
	} `json:"accounts"`
}

func fetchrelay-history(ctx context.Context, client *http.Client, root *url.URL, key, tz string) (Report, error) {
	q := url.Values{}
	loc := time.Local
	if l, err := time.LoadLocation(tz); tz != "" && err == nil {
		q.Set("timezone", tz)
		loc = l
	}
	req, _ := http.NewRequest("GET", endpoint(root, "/me/usage", q), nil)
	req.Header.Set("Authorization", "Bearer "+key)
	var a asterReport
	code, err := getJSON(ctx, client, req, &a)
	if err != nil {
		if code == 404 || code == 405 {
			return Report{}, errNotrelay-history
		}
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			return Report{}, errNotrelay-history
		}
		return Report{}, fmt.Errorf("relay-history：%w", err)
	}
	if a.Service != "relay-history" {
		return Report{}, errNotrelay-history
	}
	return relay-historyReport(a, time.Now(), loc), nil
}

// relay-historyReport folds a /me/usage document into a Report; times the user reads (when a pool is back) are in loc.
func relay-historyReport(a asterReport, now time.Time, loc *time.Location) Report {
	r := Report{Gateway: "relay-history", KeyName: a.APIKey, CheckedAt: now.UTC()}
	for _, b := range []struct {
		label string
		b     *asterBucket
	}{{"今日", a.Today}, {"本周", a.Week}, {"本月", a.Month}} {
		if b.b != nil {
			r.Usage = append(r.Usage, Usage{Label: b.label, Requests: b.b.Requests, Tokens: b.b.BilledTokens, CostUSD: b.b.CostUSD})
		}
	}
	if b := a.Budget; b != nil {
		if b.LimitUSD > 0 {
			r.Budget = append(r.Budget, usdWindow("累计预算", b.SpentUSD, b.LimitUSD, nil, b.Exhausted))
		}
		if w := b.Weekly; w != nil && w.LimitUSD > 0 {
			r.Budget = append(r.Budget, usdWindow("每周预算", w.SpentUSD, w.LimitUSD, w.ResetsAt, w.Exhausted))
		}
	}
	if a.AccountQuotaShared != nil && !*a.AccountQuotaShared {
		r.Note = "这个 Key 没有开放账号额度（relay-history 的「额度可见性」是「不开放」）"
		return r
	}
	for _, acc := range a.Accounts {
		name := acc.Provider
		if acc.Plan != "" && acc.Plan != "unknown" {
			name += " · " + acc.Plan
		}
		p := Pool{Name: name, State: acc.State, Detail: asterState(acc.State, acc.AvailableAt, loc)}
		for _, w := range acc.Windows {
			if w.Scope == "model" {
				continue // per-model windows are many and small; the account's own are what tells
			}
			p.Windows = append(p.Windows, asterWindowOf(w))
		}
		p.Windows = dedupe(p.Windows)
		sortWindows(p.Windows)
		r.Pools = append(r.Pools, p)
	}
	// Several accounts of one provider and plan (three kiro seats): tell them apart by the end of their id.
	count := map[string]int{}
	for _, p := range r.Pools {
		count[p.Name]++
	}
	for i, acc := range a.Accounts {
		if id := acc.AccountID; count[r.Pools[i].Name] > 1 && len(id) >= 6 {
			r.Pools[i].Name += " #" + id[len(id)-6:]
		}
	}
	return r
}

var asterKinds = map[string]string{"five_hour": "5 小时", "one_hour": "1 小时", "weekly": "每周", "daily": "每天", "monthly": "每月"}

// asterKindLabel names a window kind; provider-prefixed kinds (traex_weekly) by what they end with.
func asterKindLabel(kind string) string {
	if l := asterKinds[kind]; l != "" {
		return l
	}
	for k, l := range asterKinds {
		if strings.HasSuffix(kind, "_"+k) {
			return l
		}
	}
	return kind
}

func asterWindowOf(w asterWindow) Window {
	label := asterKindLabel(w.Kind)
	out := Window{Label: label, UsedRatio: w.UsedRatio, ResetsAt: w.ResetsAt, Unit: "percent"}
	if w.Limit != nil && w.Remaining != nil {
		used := *w.Limit - *w.Remaining
		out.Used, out.Limit, out.Unit = &used, w.Limit, "credits"
	}
	if w.UsedRatio != nil && *w.UsedRatio >= 1 {
		out.Exhausted = true
	}
	return out
}

func asterState(state string, at *time.Time, loc *time.Location) string {
	s := map[string]string{"available": "可用", "cooldown": "冷却中", "exhausted": "额度用完", "restricted": "暂不可用（额度闸）",
		"unavailable": "不可用"}[state]
	if s == "" {
		s = state
	}
	if at != nil && state != "available" {
		s += "，" + at.In(loc).Format("01-02 15:04") + " 恢复"
	}
	return s
}

func usdWindow(label string, spent, limit float64, resets *time.Time, exhausted bool) Window {
	ratio := spent / limit
	return Window{Label: label, UsedRatio: &ratio, Used: &spent, Limit: &limit, Unit: "usd", ResetsAt: resets, Exhausted: exhausted}
}

// ---- FAKEMODEL ----

type fmLimit struct {
	Key           string     `json:"key"`
	Label         string     `json:"label"`
	Unit          string     `json:"unit"`
	Limit         *float64   `json:"limit"`
	Used          *float64   `json:"used"`
	ResetsAt      *time.Time `json:"resets_at"`
	Source        string     `json:"source"`
	Unlimited     bool       `json:"unlimited"`
	LiveExhausted bool       `json:"live_exhausted"`
}

type fmQuota struct {
	Data []struct {
		ProviderName string    `json:"provider_name"`
		ProviderType string    `json:"provider_type"`
		Status       string    `json:"status"`
		Message      string    `json:"message"`
		Limits       []fmLimit `json:"limits"`
		Models       []struct {
			ModelID string    `json:"model_id"`
			Limits  []fmLimit `json:"limits"`
		} `json:"models"`
	} `json:"data"`
}

func fetchFakemodel(ctx context.Context, client *http.Client, root *url.URL, key string) (Report, error) {
	jar, _ := cookiejar.New(nil)
	c := *client
	c.Jar = jar
	body, _ := json.Marshal(map[string]string{"key": key})
	req, _ := http.NewRequest("POST", endpoint(root, "/auth/key-login", nil), strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	var login struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if _, err := getJSON(ctx, &c, req, &login); err != nil {
		return Report{}, fmt.Errorf("FAKEMODEL 登录：%w", err)
	}
	req, _ = http.NewRequest("GET", endpoint(root, "/api/quota", nil), nil)
	var q fmQuota
	if _, err := getJSON(ctx, &c, req, &q); err != nil {
		return Report{}, fmt.Errorf("FAKEMODEL 额度：%w", err)
	}
	// The session is read-only and lasts 8 hours; end it so they do not pile up.
	req, _ = http.NewRequest("POST", endpoint(root, "/auth/logout", nil), nil)
	if resp, err := c.Do(req.WithContext(ctx)); err == nil {
		resp.Body.Close()
	}
	return FakemodelReport(q, time.Now()), nil
}

var fmStates = map[string]string{"ok": "", "partial": "部分额度没读到", "error": "上游额度读不到，只有网关统计", "unsupported": "上游没有额度接口，只有网关统计"}

// FakemodelReport folds /api/quota into a Report: each provider with the windows that have a limit (the gateway's
// own counts without a limit say nothing about what is left), its models' limited windows under it.
func FakemodelReport(q fmQuota, now time.Time) Report {
	r := Report{Gateway: "fakemodel", CheckedAt: now.UTC()}
	for _, p := range q.Data {
		pool := Pool{Name: p.ProviderName, State: p.Status, Detail: fmStates[p.Status]}
		if p.Message != "" && p.Status != "ok" {
			pool.Detail = p.Message
		}
		pool.Windows = fmWindows(p.Limits)
		for _, m := range p.Models {
			if ws := fmWindows(m.Limits); len(ws) > 0 {
				pool.Models = append(pool.Models, ModelWindows{Model: m.ModelID, Windows: ws})
			}
		}
		r.Pools = append(r.Pools, pool)
	}
	return r
}

func fmWindows(ls []fmLimit) []Window {
	var out []Window
	for _, l := range ls {
		if l.Limit == nil && !l.LiveExhausted {
			continue
		}
		w := Window{Label: l.Label, Used: l.Used, Limit: l.Limit, Unit: l.Unit, ResetsAt: l.ResetsAt, Exhausted: l.LiveExhausted}
		if l.Limit != nil && *l.Limit > 0 && l.Used != nil {
			ratio := *l.Used / *l.Limit
			w.UsedRatio = &ratio
			w.Exhausted = w.Exhausted || ratio >= 1
		}
		out = append(out, w)
	}
	out = dedupe(out)
	sortWindows(out)
	return out
}

// dedupe keeps one window per label (the one that says most) and drops windows that say nothing: an account can
// report the same window twice (its own and a seat's), one of them without numbers.
func dedupe(ws []Window) []Window {
	score := func(w Window) int {
		n := 0
		if w.UsedRatio != nil {
			n += 2
		}
		if w.Limit != nil {
			n++
		}
		return n
	}
	var out []Window
	at := map[string]int{}
	for _, w := range ws {
		if score(w) == 0 && !w.Exhausted {
			continue
		}
		if i, ok := at[w.Label]; ok {
			if score(w) > score(out[i]) {
				out[i] = w
			}
			continue
		}
		at[w.Label] = len(out)
		out = append(out, w)
	}
	return out
}

var windowOrder = map[string]int{"1 小时": 0, "5 小时": 1, "5 小时额度": 1, "每天": 2, "每周": 3, "每周额度": 3, "每月": 4, "每月额度": 4}

func sortWindows(ws []Window) {
	sort.SliceStable(ws, func(i, j int) bool {
		a, okA := windowOrder[ws[i].Label]
		b, okB := windowOrder[ws[j].Label]
		if !okA {
			a = 9
		}
		if !okB {
			b = 9
		}
		return a < b
	})
}
