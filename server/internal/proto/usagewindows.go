package proto

import (
	"fmt"
	"time"
)

// Subscription rate-limit windows (#2432, docs/usage-windows.md): how much of an account's 5-hour and weekly allowance
// is used. A node reads them from the agents' own local files (Codex's rollouts, a Claude Code status line drop) and
// answers usage.windows; the hub merges the machines' answers into one list per account. Nothing here carries a
// credential: the account has no id the agent's vendor knows, only the windows it was seen with.

// UsageWindow is one rate-limit window of one account.
type UsageWindow struct {
	// Label is the window's length as people say it: "5h", "7d" (see WindowLabel).
	Label         string  `json:"label"`
	WindowMinutes int     `json:"window_minutes"`
	UsedPercent   float64 `json:"used_percent"`
	// ResetsAt is when the window starts over; nil when the source did not say.
	ResetsAt *time.Time `json:"resets_at,omitempty"`
}

// UsageAccount is one subscription account as one source saw it (a node's answer) or as the hub merged it.
type UsageAccount struct {
	// ID is set by the hub: a short digest of the provider and the windows' identity, stable while the windows are.
	ID       string `json:"id,omitempty"`
	Provider string `json:"provider"`       // codex | claude
	Name     string `json:"name"`           // "Codex Pro", "Claude"
	Plan     string `json:"plan,omitempty"` // as the source calls it ("pro")
	// Windows are ordered shortest first.
	Windows []UsageWindow `json:"windows"`
	// SampledAt is when the source wrote the newest of these numbers (not when we read them).
	SampledAt time.Time `json:"sampled_at"`
	// Stale: the sample is older than the collector's schedule allows (its source has not been written to for a
	// while, or the machine that had it is offline).
	Stale bool `json:"stale,omitempty"`
}

// UsageSnapshot is what a node answers to usage.windows.
type UsageSnapshot struct {
	// Enabled false: hub.json machines[].usage_windows is false for this machine; nothing is read.
	Enabled     bool           `json:"enabled"`
	CollectedAt time.Time      `json:"collected_at"`
	Accounts    []UsageAccount `json:"accounts"`
}

// WindowLabel names a window by its length: 300 → "5h", 10080 → "7d", 45 → "45m"; 0 or less → "".
func WindowLabel(minutes int) string {
	switch {
	case minutes <= 0:
		return ""
	case minutes%(24*60) == 0:
		return fmt.Sprintf("%dd", minutes/(24*60))
	case minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
}
