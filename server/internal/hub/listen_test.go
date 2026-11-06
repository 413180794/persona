package hub

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// TestUnclaimedInherited: a socket systemd passed for a port hub.json no longer listens on stays unclaimed, and the
// hub says so, until something listens on that port.
func TestUnclaimedInherited(t *testing.T) {
	a, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	inherited.once.Do(func() {}) // as if the process had taken LISTEN_FDS already
	inherited.mu.Lock()
	saved := inherited.lns
	inherited.lns = []net.Listener{a, b}
	inherited.mu.Unlock()
	t.Cleanup(func() {
		inherited.mu.Lock()
		inherited.lns = saved
		inherited.mu.Unlock()
	})

	ln, activated, err := listenTCP(a.Addr().String())
	if err != nil || !activated || ln != a {
		t.Fatalf("listenTCP(%s) = %v, %v, %v; want the passed socket", a.Addr(), ln, activated, err)
	}
	got := unclaimedInherited()
	if len(got) != 1 || got[0].String() != b.Addr().String() {
		t.Fatalf("unclaimedInherited() = %v, want [%s]", got, b.Addr())
	}
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	logUnclaimedInherited(logf)
	if len(logs) != 1 || !strings.Contains(logs[0], b.Addr().String()) || !strings.Contains(logs[0], "personad hub setup") {
		t.Fatalf("log = %q, want one line naming %s and the fix", logs, b.Addr())
	}

	if _, activated, err := listenTCP(b.Addr().String()); err != nil || !activated {
		t.Fatalf("listenTCP(%s): %v, %v", b.Addr(), activated, err)
	}
	if got := unclaimedInherited(); len(got) != 0 {
		t.Fatalf("unclaimedInherited() after both ports = %v, want none", got)
	}
	logs = nil
	logUnclaimedInherited(logf)
	if len(logs) != 0 {
		t.Fatalf("log with every socket claimed = %q", logs)
	}
}
