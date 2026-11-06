package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/mdns"
)

// `jarvis-link discover`: the hubs on this LAN, as they advertise themselves over mDNS (_jarvis-hub._tcp, docs/mesh.md).
// Finding the hub is one of the connector's three jobs; the installer asks it whether this computer needs a hub of its
// own or the node service is enough (docs/plan-hub-center.md section 0). Nothing is paired by it: pairing takes a link.

const hubService = "_jarvis-hub._tcp"

// foundHub is a hub heard on the LAN: what its TXT record says (never a credential) and where it is.
type foundHub struct {
	HubID     string   `json:"hub_id"`
	Name      string   `json:"name,omitempty"`
	Version   string   `json:"version,omitempty"`
	Addresses []string `json:"addresses"` // https://host:tls-port, LAN first
}

// browseHubs listens for hubs for d (tests replace it).
var browseHubs = func(ctx context.Context, d time.Duration) ([]foundHub, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	var mu sync.Mutex
	hubs := map[string]foundHub{}
	err := mdns.Run(ctx, mdns.Options{Browse: hubService, QueryEvery: time.Second, Found: func(e mdns.Entry) {
		id := e.TXT["hub_id"]
		if id == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if e.TTL == 0 {
			delete(hubs, id)
			return
		}
		port := e.Port
		if p, err := strconv.Atoi(e.TXT["tls"]); err == nil && p > 0 {
			port = p
		}
		h := foundHub{HubID: id, Name: clip(e.TXT["name"], 60), Version: clip(e.TXT["v"], 60)}
		for _, a := range e.Addrs {
			if a.Is6() && a.Zone() != "" {
				continue // link-local: only usable with its interface
			}
			h.Addresses = append(h.Addresses, "https://"+net.JoinHostPort(a.Unmap().String(), strconv.Itoa(port)))
		}
		slices.SortStableFunc(h.Addresses, func(a, b string) int { return addressRank(a) - addressRank(b) })
		hubs[id] = h
	}})
	if err != nil {
		return nil, err
	}
	out := make([]foundHub, 0, len(hubs))
	for _, h := range hubs {
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b foundHub) int {
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		if a.HubID < b.HubID {
			return -1
		}
		return 1
	})
	return out, nil
}

// addressRank puts LAN addresses first, as the link tries them.
func addressRank(u string) int {
	if k := link.KindOf(u); k == "lan" {
		return 0
	} else if k == "ipv6" {
		return 1
	}
	return 2
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// discover is `jarvis-link discover [--wait <seconds>] [--json]`: exit code 0 when a hub was heard, 1 when none.
func discover(e *env, args []string) error {
	wait, asJSON := 3*time.Second, false
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" {
			asJSON = true
			continue
		}
		v, next, ok, err := flagValue(args, i, "--wait")
		switch {
		case err != nil:
			return err
		case !ok:
			return errUsage
		}
		s, err := strconv.ParseFloat(v, 64)
		if err != nil || s <= 0 || s > 60 {
			return fmt.Errorf("--wait %q：要 0 到 60 秒", v)
		}
		wait, i = time.Duration(s*float64(time.Second)), next
	}
	hubs, err := browseHubs(context.Background(), wait)
	if err != nil {
		return fmt.Errorf("在局域网里找不了 hub（mDNS）：%w", err)
	}
	if hubs == nil {
		hubs = []foundHub{}
	}
	if asJSON {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(hubs); err != nil {
			return err
		}
	} else if len(hubs) == 0 {
		fmt.Fprintf(e.stdout, "%s内局域网里没听到 hub（mDNS；Docker 桥接网络里的 hub 听不到）。\n", duration(wait))
	} else {
		for _, h := range hubs {
			name := h.Name
			if name == "" {
				name = h.HubID
			}
			addr := ""
			if len(h.Addresses) > 0 {
				addr = " · " + h.Addresses[0]
			}
			fmt.Fprintf(e.stdout, "hub「%s」%s\n", name, addr)
		}
	}
	if len(hubs) == 0 {
		return exitCode(1)
	}
	return nil
}
