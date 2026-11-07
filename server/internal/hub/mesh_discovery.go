package hub

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/mdns"
)

// Finding other hubs on the LAN (docs/archive/operations/plan-hub-mesh.md 2.1): every hub with TLS advertises _persona-hub._tcp over
// mDNS, its TXT holding only hub_id, name, TLS port, key pin and version (never a credential), and lists the hubs it
// hears for the admin page. Nothing is paired by itself: that takes the other hub's code. Docker's bridge network does
// not pass multicast (network_mode: host does); there the other hub is added by its address.

const meshService = "_persona-hub._tcp"

func (h *Hub) runDiscovery(ctx context.Context) {
	// The TLS port and pin are known once startPublic ran; a hub without TLS cannot be paired, so it only listens.
	port, fp := h.tlsPin()
	var adv *mdns.Service
	if port > 0 && fp != "" && !loopbackOnly(h.cfg.Public.TLSListen) {
		adv = &mdns.Service{Instance: h.hubID(), Type: meshService, Host: "persona-" + shortID(h.hubID()), Port: port,
			TXT: map[string]string{"hub_id": h.hubID(), "name": clip(h.hubName(), 60), "tls": strconv.Itoa(port), "fp": fp, "v": clip(h.version, 60)}}
	}
	// Both services carry the same hub identity and pin. Discovery is not trust
	// or automatic pairing; found results are deduplicated by the existing hub id.
	var legacyAdv *mdns.Service
	if adv != nil {
		copy := *adv
		copy.Type = compat.DiscoveryService
		legacyAdv = &copy
	}
	discovery := &compatibleDiscovery{publish: h.discovered}
	go func() {
		if err := mdns.Run(ctx, mdns.Options{Advertise: legacyAdv, Browse: compat.DiscoveryService, Found: discovery.found(compat.DiscoveryService),
			Logf: func(format string, args ...any) { h.logf("mdns: "+format, args...) }}); err != nil && ctx.Err() == nil {
			h.logf("mesh: compatibility discovery off: %v", err)
		}
	}()
	err := mdns.Run(ctx, mdns.Options{Advertise: adv, Browse: meshService, Found: discovery.found(meshService),
		Logf: func(format string, args ...any) { h.logf("mdns: "+format, args...) }})
	if err != nil && ctx.Err() == nil {
		h.logf("mesh: LAN discovery off: %v", err)
	}
}

// loopbackOnly: a listen address only this machine can reach (127.0.0.1:18662, [::1]:18662, localhost:18662). Such a
// hub is not advertised: the LAN would be handed addresses where nothing listens.
func loopbackOnly(listen string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// discovered keeps (or, on a goodbye, drops) a hub heard on the LAN.
func (h *Hub) discovered(e mdns.Entry) {
	id := e.TXT["hub_id"]
	if id == "" || id == h.hubID() {
		return
	}
	ms := &h.mesh
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.discovered == nil {
		ms.discovered = map[string]discoveredHub{}
	}
	if e.TTL == 0 {
		delete(ms.discovered, id)
		return
	}
	if _, known := ms.discovered[id]; !known && len(ms.discovered) >= 64 {
		return // a LAN full of them: the ones already listed are enough
	}
	port := e.Port
	if p, err := strconv.Atoi(e.TXT["tls"]); err == nil && p > 0 {
		port = p
	}
	d := discoveredHub{HubID: id, Name: e.TXT["name"], TLSPort: port, FP: e.TXT["fp"], Version: e.TXT["v"], Seen: time.Now()}
	for _, a := range e.Addrs {
		if a.Is6() && a.Zone() != "" {
			continue // a link-local address only works with its interface: no use to the admin page
		}
		d.Addresses = append(d.Addresses, "https://"+net.JoinHostPort(a.Unmap().String(), strconv.Itoa(port)))
	}
	sort.Slice(d.Addresses, func(i, j int) bool { return endpointKindOf(d.Addresses[i]) < endpointKindOf(d.Addresses[j]) })
	ms.discovered[id] = d
}

// discoveredHubs is what the admin page lists as found on the LAN: not this hub, not the paired ones, heard lately.
func (h *Hub) discoveredHubs() []discoveredHub {
	ms := &h.mesh
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := []discoveredHub{}
	for id, d := range ms.discovered {
		if _, paired := ms.peers[id]; paired || time.Since(d.Seen) > 10*time.Minute {
			continue
		}
		if d.Addresses == nil {
			d.Addresses = []string{}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || out[i].Name == out[j].Name && out[i].HubID < out[j].HubID
	})
	return out
}
