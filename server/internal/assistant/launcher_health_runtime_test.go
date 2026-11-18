package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// A runtime's launcher (agy, not startable, so not in the periodic rounds' list) keeps its failing record through a
// check now that names no launcher, and a check now that names it probes it and takes a pass back to ok. A record of a
// launcher the machine no longer lists still goes.
func TestCheckNowKeepsAndProbesRuntimeLaunchers(t *testing.T) {
	s, f := launcherHealthHub(t)
	f.mu.Lock()
	f.launcherList = []proto.Launcher{{Name: "work"}, {Name: "agy", Source: "dsh", Agent: "agy"}}
	f.checks["l4/agy"] = proto.LauncherCheck{Agent: "agy", Error: `API Error: 401 {"error":"invalid or missing API key"}`}
	f.mu.Unlock()
	ctx := context.Background()
	s.recordHealth(ctx, "l4", "agy", healthResult{err: `API Error: 401 {"error":"invalid or missing API key"}`, source: healthSourceStart})
	s.recordHealth(ctx, "l4", "gone", healthResult{err: "launcher missing", source: healthSourceStart})

	probed := map[string]int{}
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, params json.RawMessage) (any, error) {
		var p struct{ Name string }
		_ = json.Unmarshal(params, &p)
		probed[p.Name]++
		if p.Name == "agy" {
			return proto.LauncherCheck{OK: true, Agent: "agy", Model: "m"}, nil
		}
		return proto.LauncherCheck{Error: "unexpected probe"}, nil
	})

	// Check now without a launcher: agy is not the main chat's or the Agents' default, so it is not probed and keeps its record.
	postJSON(t, s, "/api/v1/launchers/health/check?machine=l4")
	if r := healthState(s, "l4", "agy"); r.State != healthFailing || probed["agy"] != 0 {
		t.Fatalf("check now without a launcher touched a runtime's record (probes=%v): %+v", probed, r)
	}
	if _, ok := s.launcherHealth(ctx, "l4", "gone"); ok {
		t.Fatal("a record of a launcher the machine no longer lists was kept")
	}

	// Check now that names it: probed, and the pass clears the failure.
	postJSON(t, s, "/api/v1/launchers/health/check?machine=l4&launcher=agy")
	if r := healthState(s, "l4", "agy"); r.State != healthOK || probed["agy"] != 1 {
		t.Fatalf("check now of the runtime launcher (probes=%v): %+v", probed, r)
	}

	// Made the Agents' default, it is checked by the rounds without a launcher named.
	setModel(t, s, "agent.new", ModelBinding{Provider: "launcher.agy"})
	postJSON(t, s, "/api/v1/launchers/health/check?machine=l4")
	if probed["agy"] != 2 {
		t.Fatalf("the Agents' default runtime launcher was not checked (probes=%v)", probed)
	}
}

// runtimeChecks names only what the machine lists and never an external launcher.
func TestRuntimeChecksSkipExternalAndUnlisted(t *testing.T) {
	s, _ := launcherHealthHub(t)
	ext := proto.ExternalLauncherPrefix + "x"
	list := []proto.Launcher{{Name: "agy", Source: "dsh"}, {Name: ext, Source: proto.ExternalLauncherSource}}
	ctx := context.Background()
	if got := s.runtimeChecks(ctx, list, ext); got != nil {
		t.Fatalf("an external launcher was named: %v", got)
	}
	if got := s.runtimeChecks(ctx, list, "agy"); len(got) != 1 || got[0] != "agy" {
		t.Fatalf("the listed runtime launcher was not named: %v", got)
	}
	if got := s.runtimeChecks(ctx, list[1:], "agy"); got != nil {
		t.Fatalf("an unlisted launcher was named: %v", got)
	}
	if got := s.runtimeChecks(ctx, list, ""); strings.Join(got, ",") != "" {
		t.Fatalf("a runtime launcher no one defaults to was named by a round: %v", got)
	}
}

// A passing readiness probe takes a failing record back to ok as a passing Test does; a failed probe records nothing,
// and a passing one creates no record where there was none.
func TestReadinessProbePassClearsFailure(t *testing.T) {
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	ctx := context.Background()
	s.recordHealth(ctx, "server", "shared", healthResult{err: `API Error: 401 {"error":"invalid or missing API key"}`, source: healthSourceStart})
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, _ json.RawMessage) (any, error) {
		return proto.LauncherCheck{Error: "still broken"}, nil
	})
	s.readinessProbe(ctx, "server", "shared")
	if r := healthState(s, "server", "shared"); r.State != healthFailing || r.Source != healthSourceStart {
		t.Fatalf("a failed probe changed the record: %+v", r)
	}

	// The failed answer is cached for readinessProbeReuse: forget it, as a later try after that would.
	s.readinessUpdate(ctx, func(r *readinessRecord) { r.Probe = nil })
	f.answerNode(assistantwire.DeviceLaunchersCheck, func(_ string, _ json.RawMessage) (any, error) {
		return proto.LauncherCheck{OK: true, Model: "m"}, nil
	})
	s.readinessProbe(ctx, "server", "shared")
	if r := healthState(s, "server", "shared"); r.State != healthOK || r.Source != healthSourceCheck {
		t.Fatalf("a passing probe did not clear the failure: %+v", r)
	}
	s.readinessProbe(ctx, "server", "other")
	if _, ok := s.launcherHealth(ctx, "server", "other"); ok {
		t.Fatal("a passing probe with no record created one")
	}
}
