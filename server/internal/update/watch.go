package update

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Health is what the watchdog reads from the hub's /api/v1/health.
type Health struct {
	Build  int64 `json:"build"`
	Update struct {
		Confirmed bool `json:"confirmed"` // this process finished an update's checks (or had none to do)
	} `json:"update"`
}

// Watch is the watchdog `jarvisd update-watch` runs: the old binary's code, started just before the hub runs the new
// one. It waits until the hub answers as the new build with the update confirmed; by deadline it has not (the new
// version hangs, or dies before it can roll back by itself), it restores the old binary, writes why, and restarts
// the hub on it. Someone else finishing the update (the pending marker gone) ends it too.
func Watch(ctx context.Context, client *http.Client, p Pending, pendingPath string, deadline time.Time, every time.Duration,
	restore func() error, restart func() error) error {
	for {
		if _, err := os.Stat(pendingPath); os.IsNotExist(err) {
			return nil // confirmed, or rolled back by the new version itself
		}
		if h, err := health(ctx, client, p.HealthURL); err == nil {
			if h.Build == p.ToBuild && h.Update.Confirmed {
				return nil
			}
			if h.Build != 0 && h.Build != p.ToBuild && h.Build != p.FromBuild {
				return nil // a build deployed straight from master took over meanwhile: not this watchdog's to undo
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
	if err := restore(); err != nil {
		return err
	}
	_ = Save(RolledBackPath(filepath.Dir(pendingPath)), RolledBack{FromVersion: p.FromVersion, ToVersion: p.ToVersion, ToBuild: p.ToBuild,
		Started: p.Started, At: time.Now(), Reason: "新版本 3 分钟内没有起来并确认（看门狗回滚）", Auto: p.Auto})
	_ = os.Remove(pendingPath)
	return restart()
}

func health(ctx context.Context, client *http.Client, url string) (Health, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var h Health
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return h, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	return h, json.NewDecoder(resp.Body).Decode(&h)
}
