package hub

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/tunnel"
)

// Reaching a machine (docs/server.md「tailcat 打洞」): ssh when the hub can, tailcat when it cannot. A machine with
// both (ssh_host and tailcat_addr) tries ssh first; when ssh fails, tailcat right after. While it runs over tailcat
// the hub checks ssh every few minutes and goes back to it once it works: ssh is the plain, well-known way, and
// tailcat may go through Tailscale's shared relays.

const (
	// directRetry: after ssh failed, reconnects go to tailcat first for this long instead of waiting on ssh again.
	directRetry = 5 * time.Minute
	// directProbeEvery: how often a machine on tailcat checks whether ssh works again; doubled for every time going
	// back to ssh failed right away (ssh logs in but the node does not answer over it), up to 32 times.
	directProbeEvery = 5 * time.Minute
	maxFlaps         = 5
	// tailcatCheckEvery: how often a connection over tailcat is pinged (its userspace TCP has no keepalive: a
	// machine gone to sleep or off the network would stay "online" until a write gave up, minutes later) and its
	// path (direct / relay) asked for, which also nudges NAT traversal. Two missed pings end the connection.
	tailcatCheckEvery = time.Minute
)

// tailcatTimings are a machine's (tests shorten them).
type tailcatTimings struct {
	directRetry, directProbeEvery, checkEvery time.Duration
}

var defaultTailcatTimings = tailcatTimings{directRetry, directProbeEvery, tailcatCheckEvery}

// probeEvery is how long to wait before checking ssh again, longer after each failed switch back.
func (m *Machine) probeEvery() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.timings.directProbeEvery << min(m.flaps, maxFlaps)
}

// noteDirectFailed: ssh did not get to the node. Right after going back to it from tailcat, that is a flap.
func (m *Machine) noteDirectFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.directFailed = time.Now()
	if !m.switchedBack.IsZero() && time.Since(m.switchedBack) < 2*time.Minute {
		m.flaps = min(m.flaps+1, maxFlaps)
	}
	m.switchedBack = time.Time{}
}

// transports are the ways to try, in order. A link machine is only reached through its jarvis-link (link.go; tests
// give it a node command instead, run like ssh's).
func (m *Machine) transports() []string {
	switch {
	case m.cfg.Kind == "local":
		return []string{"local"}
	case m.cfg.Kind == kindLink && m.nodeCmd == nil:
		return []string{"link"}
	case m.cfg.Kind == kindService && m.nodeCmd == nil:
		return []string{kindService}
	case m.cfg.TailcatAddr == "":
		return []string{"ssh"}
	case m.cfg.Kind == "tailcat":
		return []string{"tailcat"}
	}
	m.mu.Lock()
	recent := !m.directFailed.IsZero() && time.Since(m.directFailed) < m.timings.directRetry
	m.mu.Unlock()
	if recent {
		return []string{"tailcat", "ssh"}
	}
	return []string{"ssh", "tailcat"}
}

// tailcatDialer is the hub's tailcat client, made the first time a machine needs it: the hub's key file is created
// then (`jarvisd hub tailcat-key` prints it for the nodes).
type tailcatDialer struct {
	once sync.Once
	d    *tunnel.Dialer
	err  error
}

func (h *Hub) tailcatClient() (*tunnel.Dialer, error) {
	t := &h.tailcat
	t.once.Do(func() {
		k, err := tunnel.LoadOrCreateHubKey(tunnel.HubKeyPath())
		if err != nil {
			t.err = err
			return
		}
		t.d = &tunnel.Dialer{Key: k.Key, DERPMapURL: h.cfg.Tailcat.DERPMapURL, Logf: h.logf}
	})
	return t.d, t.err
}

func (m *Machine) tailcatDial(ctx context.Context) (net.Conn, error) {
	if m.dialTailcat != nil {
		return m.dialTailcat(ctx)
	}
	d, err := m.hub.tailcatClient()
	if err != nil {
		return nil, err
	}
	c, err := d.Dial(ctx, m.cfg.TailcatAddr, tunnel.NodePort)
	if errors.Is(err, context.DeadlineExceeded) {
		// What a node not running, not set up for this hub's key, or not listening yet looks like: no answer at all.
		return nil, errors.New("tailcat 连不上：那台电脑的常驻 node 没在跑，或没有加这个 hub 的 key（jarvisd node tailcat init）")
	}
	return c, err
}

// tailcatReset starts a new tunnel next time: the node may have moved networks or restarted.
func (m *Machine) tailcatReset() {
	if m.dialTailcat != nil {
		return
	}
	if d, _ := m.hub.tailcatClient(); d != nil {
		d.Reset(m.cfg.TailcatAddr)
	}
}

// watchTailcat runs while a connection over tailcat lasts: it pings the node (ending a dead connection), keeps the
// path up to date, and goes back to ssh when that works again (only between interactions: a reconnect would cancel
// a question waiting on the phone).
//
// A connection that moved bytes lately is alive: a ping is only sent when it was idle for a whole check, and the
// watcher never waits on it longer than a check (the ping queues behind any big write: on a slow relay that takes a
// while, on a dead connection forever). Two idle checks in a row without an answer end the connection; closing it
// also fails the stuck write.
func (m *Machine) watchTailcat(ctx context.Context, peer *rpc.Peer, activity *activityConn) {
	check := time.NewTicker(m.timings.checkEvery)
	defer check.Stop()
	probeTick := time.NewTicker(m.probeEvery())
	defer probeTick.Stop()
	m.updatePath(ctx)
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-check.C:
			if activity == nil || activity.idle() < m.timings.checkEvery || m.ping(ctx, peer) || activity.idle() < m.timings.checkEvery {
				misses = 0
				go m.updatePath(ctx)
				continue
			}
			if ctx.Err() != nil {
				return
			}
			if misses++; misses >= 2 {
				m.hub.logf("[%s] tailcat: no answer to two pings, reconnecting", m.cfg.ID)
				m.tailcatReset()
				peer.Close(errors.New("tailcat 连接没有回应（那台电脑睡眠或断网了？）"))
				if activity != nil {
					_ = activity.Close() // what fails a write stuck on the dead connection (connectVia may be in one)
				}
				return
			}
		case <-probeTick.C:
			if m.cfg.Kind == "tailcat" || m.hub.machinePending(m.cfg.ID) {
				continue
			}
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := m.directWorks(pctx)
			cancel()
			if err != nil {
				continue
			}
			m.mu.Lock()
			m.directFailed, m.switchedBack = time.Time{}, time.Now()
			m.mu.Unlock()
			m.hub.logf("[%s] ssh works again: switching back from tailcat", m.cfg.ID)
			peer.Close(errors.New("ssh works again: switching back from tailcat"))
			return
		}
	}
}

// ping asks the node for an answer within a check, without waiting on a ping stuck behind a write.
func (m *Machine) ping(ctx context.Context, peer *rpc.Peer) bool {
	pctx, cancel := context.WithTimeout(ctx, min(20*time.Second, m.timings.checkEvery))
	defer cancel()
	answer := make(chan error, 1)
	go func() { answer <- peer.Call(pctx, "ping", map[string]any{}, nil) }()
	select {
	case err := <-answer:
		var answered *rpc.Error // an old node without ping says "unknown method": it answered
		return err == nil || errors.As(err, &answered)
	case <-pctx.Done():
		return false
	}
}

// activityConn is a connection that notes when bytes last moved: a read returning data, or a piece of a write going
// out (writes are split so a big one on a slow link shows progress while it lasts).
type activityConn struct {
	net.Conn
	last atomic.Int64 // unix nanoseconds
}

const activityChunk = 32 << 10

func (c *activityConn) touch() { c.last.Store(time.Now().UnixNano()) }

func (c *activityConn) idle() time.Duration { return time.Since(time.Unix(0, c.last.Load())) }

func (c *activityConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

func (c *activityConn) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n, err := c.Conn.Write(p[:min(len(p), activityChunk)])
		written += n
		if n > 0 {
			c.touch()
		}
		if err != nil {
			return written, err
		}
		p = p[n:]
	}
	return written, nil
}

func (m *Machine) updatePath(ctx context.Context) {
	if m.dialTailcat != nil {
		return
	}
	d, err := m.hub.tailcatClient()
	if err != nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	path, err := d.Path(pctx, m.cfg.TailcatAddr)
	cancel()
	if err != nil {
		return
	}
	m.mu.Lock()
	changed := m.via == "tailcat" && m.path != path
	if changed {
		m.path = path
	}
	m.mu.Unlock()
	if changed {
		m.hub.logf("[%s] tailcat path: %s", m.cfg.ID, path)
		m.hub.broadcastMachines()
	}
}

// directWorks checks the hub can ssh to the machine, without starting a node.
func (m *Machine) directWorks(ctx context.Context) error {
	if m.probeDirect != nil {
		return m.probeDirect(ctx)
	}
	host := m.cfg.SSHHost
	if host == "" {
		host = m.cfg.ID
	}
	return exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, "true").Run()
}

// machinePending: a question, plan or permission from this machine is waiting on the phone.
func (h *Hub) machinePending(machineID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.interactions {
		if p.ix.MachineID == machineID {
			return true
		}
	}
	return false
}

// deployTailcat sends the hub's node build over tailcat: the node installs it over itself and exits, and its
// supervisor starts the new one.
func (m *Machine) deployTailcat(ctx context.Context, bin string) error {
	d, err := m.hub.tailcatClient()
	if err != nil {
		return err
	}
	c, err := d.Dial(ctx, m.cfg.TailcatAddr, tunnel.UploadPort)
	if err != nil {
		return err
	}
	defer c.Close()
	return tunnel.Upload(ctx, c, bin)
}

// closeTailcat ends every tunnel when the hub stops.
func (h *Hub) closeTailcat() {
	h.tailcat.once.Do(func() {}) // never made: nothing to close (and none made after this)
	if h.tailcat.d != nil {
		h.tailcat.d.Close()
	}
}
