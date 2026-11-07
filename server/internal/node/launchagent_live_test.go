//go:build launchdlive && darwin

package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Against the real launchd of this Mac, with a throwaway agent (never either resident-node label: launchd
// labels do not follow HOME, so the resident node would be the one stopped):
//
//	go test -tags launchdlive -run TestLiveLaunchAgentFix ./internal/node/
//
// It loads an agent written the old way (no ProcessType) that runs sleep, checks that launchd runs it as "daemon",
// lets the doctor's fix write ProcessType and reload it, checks "interactive", and takes the agent away again.
func TestLiveLaunchAgentFix(t *testing.T) {
	label := fmt.Sprintf("com.example.persona-doctor-live.%d", os.Getpid())
	path := filepath.Join(t.TempDir(), label+".plist")
	plist := strings.NewReplacer(NodeLaunchdLabel, label, "/Users/a&amp;b/.persona/bin/personad", "/bin/sleep",
		"<string>node</string><string>--attach</string>", "<string>600</string>",
		"/Users/a&amp;b/.persona/node-attach.log", "/dev/null").Replace(oldNodePlist)
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	domain := "gui/" + strconv.Itoa(os.Getuid())
	opts := DoctorOptions{LaunchAgent: path, LaunchdTarget: domain + "/" + label, Launchctl: runLaunchctl}
	if out, err := runLaunchctl(ctx, "bootstrap", domain, path); err != nil {
		t.Fatalf("bootstrap: %s %v", out, err)
	}
	t.Cleanup(func() {
		_, _ = runLaunchctl(context.Background(), "bootout", opts.LaunchdTarget)
		if _, err := runLaunchctl(context.Background(), "print", opts.LaunchdTarget); err == nil {
			t.Errorf("%s is still loaded", label)
		}
	})

	before := inspectLaunchAgent(ctx, opts)
	if !before.loaded || before.spawnType != "daemon" || before.processType != "" {
		t.Fatalf("before: %+v", before)
	}
	if c := checkLaunchAgent(before, opts); c.Status != DoctorWarn {
		t.Fatalf("before: %+v", c)
	}
	wrote, reloaded, err := fixLaunchAgent(ctx, opts, before)
	if err != nil || !wrote || !reloaded {
		t.Fatalf("fix: wrote %v reloaded %v %v", wrote, reloaded, err)
	}
	after := inspectLaunchAgent(ctx, opts)
	if !after.loaded || after.spawnType != "interactive" || after.processType != processInteractive {
		t.Fatalf("after: %+v", after)
	}
	c := checkLaunchAgent(after, opts)
	if c.Status != DoctorOK {
		t.Fatalf("after: %+v", c)
	}
	t.Logf("before: spawn type %s; after: %s (%s)", before.spawnType, after.spawnType, c.Detail)
}
