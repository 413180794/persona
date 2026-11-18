package node

import (
	"encoding/json"
	"math"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Only the built-in launcher using the default profile shares the status line's default source.
// Other launchers can supply their own upstream even when they share a config directory.
func claudeStreamSource(launcher, configDir, home string) string {
	if launcher == "claude" && (configDir == "" || filepath.Clean(configDir) == filepath.Join(home, ".claude")) {
		return "claude:claude.json"
	}
	return "claude-stream:" + launcher + "\x00" + configDir
}

type claudeWindowSample struct {
	window proto.UsageWindow
	at     time.Time
}

type claudeStreamWindow struct {
	source  string
	minutes int
}

// This lock is separate from the file poll: stdout also carries time-sensitive control replies.
type claudeUsageStreams struct {
	sync.Mutex
	samples map[claudeStreamWindow]claudeWindowSample
}

func newerClaudeWindow(a, b proto.UsageWindow) bool {
	if b.ResetsAt == nil { // a measured period supersedes the undated zero
		return true
	}
	if a.ResetsAt.Equal(*b.ResetsAt) {
		return a.UsedPercent > b.UsedPercent
	}
	return a.ResetsAt.After(*b.ResetsAt)
}

// A rate_limit_event can contain both unified windows and/or the currently limiting window at top level.
// Decode each independently: a malformed optional field cannot invalidate the other window.
func (n *Node) recordClaudeUsage(source string, raw json.RawMessage, now time.Time) {
	if source == "" || n.usageWin.off.Load() {
		return
	}
	var info struct {
		Type    string                     `json:"rateLimitType"`
		Unified map[string]json.RawMessage `json:"unifiedWindows"`
	}
	if json.Unmarshal(raw, &info) != nil {
		return
	}
	streams := &n.usageWin.streams
	streams.Lock()
	defer streams.Unlock()
	if n.usageWin.off.Load() {
		return
	}
	for _, def := range claudeStatusWindows {
		candidates := []json.RawMessage{info.Unified[def.key]}
		if def.key == info.Type {
			candidates = append(candidates, raw)
		}
		for _, data := range candidates {
			var sample struct {
				Used  *float64 `json:"utilization"`
				Reset int64    `json:"resetsAt"`
			}
			if len(data) == 0 || json.Unmarshal(data, &sample) != nil || sample.Used == nil || *sample.Used < 0 || sample.Reset <= now.Unix() || sample.Reset > 253402300799 {
				continue
			}
			reset := time.Unix(sample.Reset, 0).UTC()
			win := proto.UsageWindow{Label: proto.WindowLabel(def.minutes), WindowMinutes: def.minutes, UsedPercent: math.Min(*sample.Used, 1) * 100, ResetsAt: &reset}
			key := claudeStreamWindow{source, def.minutes}
			if !newerClaudeWindow(win, streams.samples[key].window) {
				continue
			}
			if streams.samples == nil {
				streams.samples = make(map[claudeStreamWindow]claudeWindowSample)
			}
			streams.samples[key] = claudeWindowSample{win, now.UTC()}
		}
	}
}

// Combine volatile event samples with the last file poll without changing the cached file sample.
func (s *claudeUsageStreams) overlay(accounts map[string]proto.UsageAccount, now time.Time) {
	s.Lock()
	defer s.Unlock()
	for key, sample := range s.samples {
		w := sample.window
		if !w.ResetsAt.After(now) || now.Sub(sample.at) > usageWindowsMaxAge {
			delete(s.samples, key)
			continue
		}
		a, ok := accounts[key.source]
		if !ok {
			a = proto.UsageAccount{Provider: "claude", Name: "Claude"}
		}
		i := slices.IndexFunc(a.Windows, func(v proto.UsageWindow) bool { return v.WindowMinutes == key.minutes })
		if i >= 0 && !newerClaudeWindow(w, a.Windows[i]) {
			continue
		}
		a.Windows = slices.Clone(a.Windows)
		if i < 0 {
			a.Windows = append(a.Windows, w)
		} else {
			a.Windows[i] = w
		}
		if sample.at.After(a.SampledAt) {
			a.SampledAt = sample.at
		}
		accounts[key.source] = a
	}
}
