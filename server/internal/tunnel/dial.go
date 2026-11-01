package tunnel

import (
	"context"
	"net"
	"sync"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
	"tailscale.com/types/logger"
)

// Dialer is the hub's side: one tailcat client per node address, all with the hub's key. A client brings its tunnel
// up on the first dial (about as long as an ssh login over the internet) and keeps it for the next ones.
type Dialer struct {
	Key        key.NodePrivate
	DERPMapURL string
	Logf       logger.Logf

	mu      sync.Mutex
	clients map[tailcat.Addr]*tailcat.Client
}

func (d *Dialer) client(addr tailcat.Addr) *tailcat.Client {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c := d.clients[addr]; c != nil {
		return c
	}
	c := &tailcat.Client{Server: addr, Key: d.Key, DERPMapURL: d.DERPMapURL, Logf: Logf(d.Logf)}
	if d.clients == nil {
		d.clients = map[tailcat.Addr]*tailcat.Client{}
	}
	d.clients[addr] = c
	return c
}

// Dial opens a TCP connection to port on the node at addr.
func (d *Dialer) Dial(ctx context.Context, addr string, port uint16) (net.Conn, error) {
	return d.client(tailcat.Addr(addr)).DialTCPPort(ctx, port)
}

// Path reports how packets reach the node now: "direct" (NAT traversal worked) or "relay" (through DERP). Asking
// also nudges the client to look for a direct path again.
func (d *Dialer) Path(ctx context.Context, addr string) (string, error) {
	r, err := d.client(tailcat.Addr(addr)).DiscoPing(ctx)
	if err != nil {
		return "", err
	}
	if r.Endpoint != "" {
		return "direct", nil
	}
	return "relay", nil
}

// Reset drops the client for addr: the next dial starts a new tunnel (after the node moved networks, or restarted
// and the old one went stale).
func (d *Dialer) Reset(addr string) {
	d.mu.Lock()
	c := d.clients[tailcat.Addr(addr)]
	delete(d.clients, tailcat.Addr(addr))
	d.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// Close ends every tunnel.
func (d *Dialer) Close() {
	d.mu.Lock()
	cs := d.clients
	d.clients = nil
	d.mu.Unlock()
	for _, c := range cs {
		_ = c.Close()
	}
}
