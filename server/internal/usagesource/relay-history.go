package usagesource

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// relay-history (github.com/nwverse/relay-history) pools provider accounts behind one gateway and stores each account's quota
// windows, harvested from the headers upstream attaches to ordinary traffic. Its management API, all under
// /api/admin with "Authorization: Bearer <management key>":
//
//	GET  /accounts?view=compact          → {"accounts": [{id, name, provider, status, …}]}  (no credentials, no email)
//	POST /accounts/usage {ids, refresh}  → {"accounts": [{account_id, plan_display_name, windows: [...], checked_at, …}]}
//
// refresh is always false here: the gateway answers from its database and never calls an upstream (a refresh would
// spend the pool's probe budget, and every probe leaves from the gateway's one address). No other route is used.
// A scoped key (an "onboarder" or "operator" group key, docs/admin-access.md) lists and reads only its group's accounts,
// so a dedicated group key is the recommended one.
const (
	relay-historyAccountsPath = "/api/admin/accounts?view=compact"
	relay-historyUsagePath    = "/api/admin/accounts/usage"
	// relay-historyBatch is how many accounts one usage call names (the gateway accepts 500; smaller calls keep each read short).
	relay-historyBatch = 100
)

type relay-historyAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
}

type relay-historyWindow struct {
	Kind        string     `json:"kind"`
	Scope       string     `json:"scope"`
	ScopedModel string     `json:"scoped_model"`
	UsedRatio   *float64   `json:"used_ratio"`
	Remaining   *float64   `json:"remaining"`
	Limit       *float64   `json:"limit"`
	ResetsAt    *time.Time `json:"resets_at"`
	CheckedAt   time.Time  `json:"checked_at"`
}

type relay-historyEntry struct {
	AccountID       string            `json:"account_id"`
	Plan            string            `json:"plan"`
	PlanRaw         string            `json:"plan_raw"`
	PlanDisplayName string            `json:"plan_display_name"`
	Windows         []relay-historyWindow `json:"windows"`
	CheckedAt       *time.Time        `json:"checked_at"`
	Error           string            `json:"error"`
	Unsupported     bool              `json:"unsupported"`
	Stale           bool              `json:"stale"`
}

func fetchrelay-history(ctx context.Context, c *client, now time.Time) (Result, error) {
	list, err := relay-historyList(ctx, c)
	if err != nil {
		return Result{}, err
	}
	res := Result{Seen: Seen{Providers: map[string]int{}, Listed: len(list)}}
	ids := make([]string, 0, len(list))
	byID := map[string]relay-historyAccount{}
	for _, a := range list {
		a.ID = strings.TrimSpace(a.ID)
		if a.ID == "" || a.Status == "disabled" { // switched off on purpose: nothing to watch
			continue
		}
		if _, dup := byID[a.ID]; dup {
			continue
		}
		if len(ids) >= AccountsMost {
			break
		}
		byID[a.ID] = a
		ids = append(ids, a.ID)
	}
	for start := 0; start < len(ids); start += relay-historyBatch {
		batch := ids[start:min(start+relay-historyBatch, len(ids))]
		body, _ := json.Marshal(map[string]any{"ids": batch, "refresh": false})
		var out struct {
			Accounts []relay-historyEntry `json:"accounts"`
		}
		if err := c.do(ctx, "POST", relay-historyUsagePath, bytes.NewReader(body), &out); err != nil {
			return Result{}, err
		}
		for _, e := range out.Accounts {
			a, ok := byID[e.AccountID]
			if !ok { // an answer for an account we did not ask about is not used
				continue
			}
			if acct, windows, ok := maprelay-history(a, e, now); ok {
				res.Accounts = append(res.Accounts, acct)
				res.Seen.Accounts++
				res.Seen.Windows += windows
				res.Seen.Providers[acct.Provider]++
			}
		}
	}
	sort.SliceStable(res.Accounts, func(i, j int) bool { return res.Accounts[i].Ref < res.Accounts[j].Ref })
	return res, nil
}

// relay-historyList reads the account list: {"accounts": [...]} from a gateway that has the compact view, a bare array from
// one before it (the fields used are the same).
func relay-historyList(ctx context.Context, c *client) ([]relay-historyAccount, error) {
	var raw json.RawMessage
	if err := c.do(ctx, "GET", relay-historyAccountsPath, nil, &raw); err != nil {
		return nil, err
	}
	var wrapped struct {
		Accounts []relay-historyAccount `json:"accounts"`
	}
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '[' {
		var arr []relay-historyAccount
		if err := json.Unmarshal(t, &arr); err != nil {
			return nil, &Error{Code: CodeBadResponse, Detail: "account list"}
		}
		return arr, nil
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, &Error{Code: CodeBadResponse, Detail: "account list"}
	}
	return wrapped.Accounts, nil
}

// maprelay-history turns one account's stored quota into a usage account; ok is false when there is nothing to draw (the
// provider publishes no usage, or no window has a length and a number). windows counts the windows kept.
func maprelay-history(a relay-historyAccount, e relay-historyEntry, now time.Time) (acct proto.UsageAccount, windows int, ok bool) {
	if e.Unsupported {
		return acct, 0, false
	}
	var list []proto.UsageWindow
	checkedAt := map[int]time.Time{}
	var newest time.Time
	for _, w := range e.Windows {
		minutes, rest, isWindow := splitKind(w.Kind)
		if !isWindow {
			continue
		}
		var used float64
		switch {
		case w.UsedRatio != nil:
			p, good := percent(*w.UsedRatio)
			if !good {
				continue
			}
			used = p
		case w.Limit != nil && w.Remaining != nil && *w.Limit > 0:
			p, good := percent(1 - *w.Remaining / *w.Limit)
			if !good {
				continue
			}
			used = p
		default:
			continue
		}
		model := strings.TrimSpace(w.ScopedModel)
		if model == "" && w.Scope == "model" {
			model = rest
		}
		out := proto.UsageWindow{Label: proto.WindowLabel(minutes), WindowMinutes: minutes, UsedPercent: used, ResetsAt: w.ResetsAt, Model: model}
		list = addWindow(list, checkedAt, out, w.CheckedAt)
		if w.CheckedAt.After(newest) {
			newest = w.CheckedAt
		}
	}
	if len(list) == 0 {
		return acct, 0, false
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].WindowMinutes != list[j].WindowMinutes {
			return list[i].WindowMinutes < list[j].WindowMinutes
		}
		return list[i].Model < list[j].Model
	})
	sampled := newest
	if e.CheckedAt != nil && e.CheckedAt.After(sampled) {
		sampled = *e.CheckedAt
	}
	if sampled.IsZero() {
		sampled = now
	}
	provider := clip(strings.ToLower(strings.TrimSpace(a.Provider)), 32)
	name := clip(strings.TrimSpace(a.Name), 80)
	if name == "" {
		name = provider
	}
	plan := clip(firstNonEmpty(e.PlanDisplayName, e.Plan, e.PlanRaw), 40)
	acct = proto.UsageAccount{
		Provider: provider, Name: name, Plan: plan, Windows: list, SampledAt: sampled.UTC(),
		Stale: e.Stale || e.Error != "", Ref: a.ID,
	}
	if e.Error != "" { // the gateway could not read the numbers just now and kept the stored ones; its text stays there
		acct.Problem = "error"
	}
	return acct, len(list), true
}

func firstNonEmpty(list ...string) string {
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// clip cuts s to at most n runes (a name from outside should not be able to fill the sidebar).
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
