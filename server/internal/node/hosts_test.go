package node

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
)

// bareHost is a host with only the core methods (no Launcher, Organizer, Streamer…), recording the keys it was sent.
type bareHost struct {
	kind string
	mu   sync.Mutex
	keys []string
}

func (h *bareHost) Kind() string                                   { return h.kind }
func (h *bareHost) Version(context.Context) string                 { return "" }
func (h *bareHost) Snapshot(context.Context) (*host.Layout, error) { return &host.Layout{}, nil }
func (h *bareHost) Agents(context.Context) ([]host.Agent, error)   { return nil, nil }
func (h *bareHost) Agent(context.Context, host.PaneRef) (*host.Agent, error) {
	return &host.Agent{}, nil
}
func (h *bareHost) Read(context.Context, host.PaneRef, host.ReadOpts) (string, error) { return "", nil }
func (h *bareHost) SendText(context.Context, host.PaneRef, string) error              { return nil }
func (h *bareHost) SendKeys(_ context.Context, _ host.PaneRef, keys ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.keys = append(h.keys, keys...)
	return nil
}
func (h *bareHost) Prompt(context.Context, host.PaneRef, string) error         { return nil }
func (h *bareHost) PaneProcesses(context.Context, host.PaneRef) ([]int, error) { return nil, nil }
func (h *bareHost) PaneEnv() string                                            { return "BARE_PANE_ID" }

func TestCloseOnHostThatCannotCloseSendsNothing(t *testing.T) {
	h := &bareHost{kind: host.Orca}
	n := &Node{hosts: host.NewRegistry(h), agents: map[string]host.Agent{"s1": {Agent: "claude", PaneID: "orca:t1:l1"}},
		state: newState()}
	err := n.close(context.Background(), "s1")
	if !errors.Is(err, host.ErrUnsupported) || !strings.HasPrefix(err.Error(), "unsupported") {
		t.Fatalf("close = %v, want unsupported", err)
	}
	if len(h.keys) != 0 {
		t.Fatalf("keys sent before finding it cannot close: %v", h.keys)
	}
}

func TestMissingCapabilityIsUnsupported(t *testing.T) {
	n := &Node{hosts: host.NewRegistry(&bareHost{kind: host.Herdr})}
	if _, _, err := paneCap[host.Organizer](n, "w1:p1"); !errors.Is(err, host.ErrUnsupported) || !strings.HasPrefix(err.Error(), "unsupported") {
		t.Fatalf("paneCap = %v", err)
	}
	if _, err := hostCap[host.Launcher](n); !errors.Is(err, host.ErrUnsupported) {
		t.Fatalf("hostCap = %v", err)
	}
	// A pane of a known host that this machine does not have is not sent to herdr.
	_, _, err := n.paneHost("orca:a:b")
	if err == nil || err.Error() != "no orca host on this machine" {
		t.Fatalf("paneHost(orca:a:b) = %v", err)
	}
	if _, err := hostCap[host.Launcher](&Node{hosts: host.NewRegistry(&bareHost{kind: host.Orca})}); err == nil ||
		err.Error() != "no herdr host on this machine" {
		t.Fatalf("hostCap without herdr = %v", err)
	}
}

func TestRefreshWithoutHostsFails(t *testing.T) {
	for _, n := range []*Node{{}, {hosts: host.NewRegistry()}} {
		if err := n.refreshHosts(context.Background()); !errors.Is(err, errNoHost) {
			t.Fatalf("refreshHosts = %v, want %v", err, errNoHost)
		}
	}
}

// The a compatible CLI pane lookup uses the host's PaneEnv; for herdr it must read an environment exactly like the regex it
// replaced.
func TestHerdrPaneEnvParsesLikeBefore(t *testing.T) {
	old := `(?:^|[\x00\s])HERDR_PANE_ID=([^\x00\s]+)`
	re := paneEnvRe((&herdrHost{}).PaneEnv())
	if re.String() != old {
		t.Fatalf("regex %q, was %q", re.String(), old)
	}
	for _, env := range []string{
		"PATH=/bin\x00HERDR_PANE_ID=w79:p2\x00HOME=/h\x00",
		"HERDR_PANE_ID=w1:p1\x00X=1",
		"/usr/bin/traex --x HOME=/h HERDR_PANE_ID=w2:p1 TERM=xterm", // ps -E
		"XHERDR_PANE_ID=w9:p9\x00",                                  // another variable ending in the name
		"HERDR_PANE_ID=\x00",
		"",
	} {
		var got, want string
		if m := re.FindStringSubmatch(env); m != nil {
			got = m[1]
		}
		if m := herdrPaneEnvRe.FindStringSubmatch(env); m != nil {
			want = m[1]
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", env, got, want)
		}
	}
	if m := re.FindStringSubmatch("A=1\x00HERDR_PANE_ID=w79:p2\x00"); m == nil || m[1] != "w79:p2" {
		t.Fatalf("parsed %v", m)
	}
}

func TestPaneOfPIDWithoutHostsCachesNothing(t *testing.T) {
	pid := os.Getpid()
	traexCache.Lock()
	delete(traexCache.panes, pid)
	traexCache.Unlock()
	if p := (&Node{}).paneOfPID(pid); p != "" {
		t.Fatalf("paneOfPID = %q", p)
	}
	traexCache.Lock()
	_, cached := traexCache.panes[pid]
	traexCache.Unlock()
	if cached {
		t.Fatal("a lookup without hosts must not be cached")
	}
}
