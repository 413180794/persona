package db

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCompareSettingRejectsStaleAndConcurrentWrites(t *testing.T) {
	d, ctx := open(t), context.Background()
	value := func(rev int64, model string) any {
		return map[string]any{"revision": rev, "binding": map[string]string{"model": model}}
	}
	if ok, err := d.CompareSetting(ctx, "model_config.test", 0, value(1, "first")); err != nil || !ok {
		t.Fatalf("create %v %v", ok, err)
	}
	if ok, err := d.CompareSetting(ctx, "model_config.test", 0, value(1, "stale")); err != nil || ok {
		t.Fatalf("stale %v %v", ok, err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := d.CompareSetting(ctx, "model_config.test", 1, value(2, "winner"))
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent winners: %d", wins)
	}
	if ok, err := d.CompareSetting(ctx, "model_config.other", 0, value(1, "other")); err != nil || !ok {
		t.Fatalf("independent key %v %v", ok, err)
	}
	var r struct{ Revision int64 }
	if _, err := d.Setting(ctx, "model_config.test", &r); err != nil || r.Revision != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestNodeModelMetadataIsPersisted(t *testing.T) {
	d, ctx := open(t), context.Background()
	s := NodeSession{Launcher: "claude", Cwd: "/tmp/assistant", StartedAt: time.Now().UTC(), Model: "specific", ModelFingerprint: "fingerprint"}
	if err := d.PutNodeSession(ctx, "machine", "session", s); err != nil {
		t.Fatal(err)
	}
	out, err := d.NodeSessions(ctx, "machine")
	if err != nil {
		t.Fatal(err)
	}
	if got := out["session"]; got.Model != s.Model || got.ModelFingerprint != s.ModelFingerprint {
		t.Fatalf("lost model: %+v", got)
	}
	s.Model, s.ModelFingerprint = "", ""
	if err := d.PutNodeSession(ctx, "machine", "session", s); err != nil {
		t.Fatal(err)
	}
	out, err = d.NodeSessions(ctx, "machine")
	if err != nil || out["session"].Model != "" {
		t.Fatalf("failed to restore default: %+v %v", out, err)
	}
}
