package proto

import "time"

// The account list of the account-pool gateways among the hub's usage sources (#4576, docs/server/usage-windows.md
// "The gateway's account list"): every account a gateway lists, disabled ones included, with its status and every quota window it
// stores, as the hub's last read of the source had them. The extensions read it with usage.gateway_accounts (the
// permission usage_gateways); the sidebar's usage strip keeps its own samples (UsageAccount). Nothing here carries a
// key, and a gateway's free text (an account's last error) is never passed on: only a short token of it.

// GatewayAccounts is usage.gateway_accounts' answer.
type GatewayAccounts struct {
	Sources  []GatewaySource  `json:"sources"`
	Accounts []GatewayAccount `json:"accounts"`
}

// GatewaySource is one usage source whose accounts are listed.
type GatewaySource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // relay (an extension that also accepts the earlier name keeps working with older hubs)
	// CheckedAt is the last read that worked; nil before the first.
	CheckedAt *time.Time `json:"checked_at"`
	// Error is the usagesource code of the last attempt when it failed ("" when it worked); the accounts are then the
	// last read that worked, marked stale.
	Error string `json:"error"`
	// Listed is how many accounts the gateway listed (up to the cap); Complete false: the list stopped short (the cap,
	// or a page that could not be read), so there may be more.
	Listed   int  `json:"listed"`
	Complete bool `json:"complete"`
}

// GatewayAccount is one account of a gateway.
type GatewayAccount struct {
	Source    string `json:"source"`     // GatewaySource.ID
	AccountID string `json:"account_id"` // the gateway's own id
	Name      string `json:"name"`
	Provider  string `json:"provider"` // the gateway's provider id: claude, codex, kiro, antigravity…
	// Plan, PlanRaw, PlanDisplayName are the gateway's ("" when it has no reading of the account).
	Plan            string `json:"plan"`
	PlanRaw         string `json:"plan_raw"`
	PlanDisplayName string `json:"plan_display_name"`
	// Status is the gateway's own: active, cooldown, exhausted, refresh_required, quarantined, expired, disabled.
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
	// Reason is the head of the gateway's last error for the account ("quarantine", "sign_in_required", a cooldown
	// reason), only when it is a short lowercase token; the rest of that text is never passed on.
	Reason        string          `json:"reason"`
	CooldownUntil *time.Time      `json:"cooldown_until"`
	Windows       []GatewayWindow `json:"windows"`
	// CheckedAt is when the gateway last read the account's numbers; Cached: it answered from its store.
	CheckedAt *time.Time `json:"checked_at"`
	Cached    bool       `json:"cached"`
	// Stale: the numbers may be old (the gateway said so, could not read them just now, has only its summary of a
	// disabled account, or the source's last read failed).
	Stale bool `json:"stale"`
}

// GatewayWindow is one stored quota window, of any kind the gateway has (five_hour, weekly, monthly, kiro_bonus,
// kiro_free_trial, kiro_overage, a model's own…), passed on as it is.
type GatewayWindow struct {
	Kind        string     `json:"kind"`
	Scope       string     `json:"scope"` // account | model | "" (not said)
	ScopedModel string     `json:"scoped_model"`
	UsedRatio   *float64   `json:"used_ratio"`
	Limit       *float64   `json:"limit"`
	Remaining   *float64   `json:"remaining"`
	ResetsAt    *time.Time `json:"resets_at"`
}
