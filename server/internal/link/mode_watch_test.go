package link

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestModeWatchObservesChangesBeforeItsGoroutineStarts(t *testing.T) {
	for _, selected := range []string{ModeActive, ModePassive} {
		t.Run(selected, func(t *testing.T) {
			actual := ModePassive
			if selected == ModePassive {
				actual = ModeActive
			}
			path := filepath.Join(t.TempDir(), "link.json")
			if _, err := SetMode(path, actual, "127.0.0.1:0"); err != nil {
				t.Fatal(err)
			}
			if got := currentMode(path); got != actual {
				t.Fatalf("mode = %s, want %s", got, actual)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go watchMode(ctx, path, selected, cancel)
			select {
			case <-ctx.Done():
			case <-time.After(2 * modeCheck):
				t.Fatal("the watcher accepted an already changed file as its initial mode")
			}
		})
	}
}
