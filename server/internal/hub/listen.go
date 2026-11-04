package hub

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
)

// Socket activation (docs/hub-restart.md「零空窗」): systemd's jarvisd.socket holds the hub's listening sockets and
// hands them to each new hub process (LISTEN_FDS). While one hub stops and the next starts, connections wait in the
// socket's backlog instead of being refused, and nothing ever sees the port closed. Without systemd (Docker, launchd,
// a hub run by hand) there are none and the hub listens itself, as before.

const listenFDsStart = 3 // SD_LISTEN_FDS_START

var inherited struct {
	once sync.Once
	mu   sync.Mutex
	lns  []net.Listener
}

// inheritedListeners takes the sockets systemd passed to this process, once: the environment is cleared so the
// processes the hub starts (nodes, plugins) do not take them for theirs.
func inheritedListeners() []net.Listener {
	inherited.once.Do(func() {
		pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
		n, err2 := strconv.Atoi(os.Getenv("LISTEN_FDS"))
		_ = os.Unsetenv("LISTEN_PID")
		_ = os.Unsetenv("LISTEN_FDS")
		_ = os.Unsetenv("LISTEN_FDNAMES")
		if err != nil || err2 != nil || pid != os.Getpid() || n <= 0 {
			return
		}
		for fd := listenFDsStart; fd < listenFDsStart+n; fd++ {
			f := os.NewFile(uintptr(fd), "listen-fd-"+strconv.Itoa(fd))
			ln, err := net.FileListener(f) // a dup with close-on-exec; the original goes
			_ = f.Close()
			if err == nil {
				inherited.lns = append(inherited.lns, ln)
			}
		}
	})
	inherited.mu.Lock()
	defer inherited.mu.Unlock()
	return inherited.lns
}

// listenTCP is the listener for addr (host:port as in hub.json): a socket systemd passed on the same port, else a
// new one. Each passed socket is handed out once.
func listenTCP(addr string) (net.Listener, bool, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, false, fmt.Errorf("listen %q: %w", addr, err)
	}
	inheritedListeners()
	inherited.mu.Lock()
	for i, ln := range inherited.lns {
		if a, ok := ln.Addr().(*net.TCPAddr); ok && strconv.Itoa(a.Port) == port {
			inherited.lns = append(inherited.lns[:i:i], inherited.lns[i+1:]...)
			inherited.mu.Unlock()
			return ln, true, nil
		}
	}
	inherited.mu.Unlock()
	ln, err := net.Listen("tcp", addr)
	return ln, false, err
}
