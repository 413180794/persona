package node

import (
	"encoding/json"
	"math"
	"os"
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
	samples   map[claudeStreamWindow]claudeWindowSample
	persistWG sync.WaitGroup
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
	if n.usageWin.off.Load() {
		streams.Unlock()
		return
	}
	changed := make(map[string]proto.UsageWindow)
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
			changed[def.key] = win
		}
	}
	streams.Unlock()
	if source == "claude:claude.json" && len(changed) > 0 {
		// File locking must not block the assistant stdout reader. Each write merges under the
		// shared lock, so concurrent goroutines and statusline writers cannot regress a window.
		streams.persistWG.Add(1)
		go func() {
			defer streams.persistWG.Done()
			if err := persistClaudeWindows(n.dir, changed); err != nil && n.logger != nil {
				n.logger.Printf("persist Claude usage windows: %v", err)
			}
		}()
	}
}

// Combine volatile event samples with the last file poll without changing the cached file sample.
func (s *claudeUsageStreams) overlay(accounts map[string]proto.UsageNodeAccount, now time.Time) {
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
			a = proto.UsageNodeAccount{UsageAccount: proto.UsageAccount{Provider: "claude", Name: "Claude"}}
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

// persistClaudeWindows uses the statusline's flock and read/merge/rename protocol.
func persistClaudeWindows(nodeDir string, updates map[string]proto.UsageWindow) error {
	dir := filepath.Join(nodeDir, "usage")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	unlock, err := lockClaudeUsage(filepath.Join(dir, "claude.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(dir, "claude.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var doc struct {
		RateLimits map[string]json.RawMessage `json:"rate_limits"`
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
	}
	if doc.RateLimits == nil {
		doc.RateLimits = make(map[string]json.RawMessage)
	}
	changed := false
	for key, win := range updates {
		var old struct {
			Used  *float64 `json:"used_percentage"`
			Reset int64    `json:"resets_at"`
		}
		_ = json.Unmarshal(doc.RateLimits[key], &old)
		if old.Used != nil && *old.Used >= 0 && old.Reset > 0 &&
			(old.Reset > win.ResetsAt.Unix() || (old.Reset == win.ResetsAt.Unix() && *old.Used >= win.UsedPercent)) {
			continue
		}
		encoded, err := json.Marshal(struct {
			Used  float64 `json:"used_percentage"`
			Reset int64   `json:"resets_at"`
		}{win.UsedPercent, win.ResetsAt.Unix()})
		if err != nil {
			return err
		}
		doc.RateLimits[key] = encoded
		changed = true
	}
	if !changed {
		return nil
	}
	data, err = json.Marshal(doc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".claude.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
