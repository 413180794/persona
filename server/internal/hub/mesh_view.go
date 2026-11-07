package hub

import (
	"sort"
)

// What every hub of a mesh group knows about the others (docs/server/mesh.md "Records"): each hub floods a record of itself —
// its links to other hubs, which hub it takes for the group's center and under which term, and the computers in its
// hub.json the center may reach through it — and works out from the records it holds who can be reached and how
// (computeView, a pure function of the records).

// hubRecord is one hub's record. A newer one (epoch, rev) replaces the older wherever it arrives.
type hubRecord struct {
	HubID     string   `json:"hub_id"`
	Name      string   `json:"name"`
	Version   string   `json:"version,omitempty"`
	Epoch     int64    `json:"epoch"` // when the hub started (Unix ns): a restarted hub's records are newer
	Rev       int64    `json:"rev"`
	TLSPort   int      `json:"tls_port,omitempty"`
	FP        string   `json:"fp,omitempty"`
	Endpoints []string `json:"endpoints,omitempty"` // its https candidates
	// PlainEndpoints: its http:// candidates for the other hubs (mesh.plaintext, peer_plain.go); never shown to apps.
	PlainEndpoints []string      `json:"plain_endpoints,omitempty"`
	Neighbors      []recNeighbor `json:"neighbors,omitempty"` // hubs it has a live link to
	// Center and Term: the group's center as this hub has it (mesh_group.go). The larger term wins everywhere.
	Center string `json:"center,omitempty"`
	Term   int64  `json:"term,omitempty"`
	// Machines: the computers in its hub.json, which the center reaches through it while it is a member.
	Machines []recMachine `json:"machines,omitempty"`
}

type recNeighbor struct {
	HubID string `json:"hub_id"`
	RTTMS int    `json:"rtt_ms,omitempty"` // the link's last ping round trip; 0 unknown
}

// recMachine is a computer as one hub has it in hub.json: enough for the center to drive its node through that hub
// (its launchers and agent settings; never how that hub reaches it: no ssh host, tailcat address or credential).
type recMachine struct {
	ID   string        `json:"id"` // its id in that hub's hub.json
	Cfg  MachineConfig `json:"cfg"`
	Kind string        `json:"kind"` // how that hub reaches it: local | ssh | tailcat | link | service
	// Avail: that hub can open a stream to the node now as far as it knows (its persona-link is up, its connector
	// answers; ssh and tailcat are tried); RTTMS: that hub's last round trip on the way there, 0 unknown.
	Avail bool `json:"avail,omitempty"`
	RTTMS int  `json:"rtt_ms,omitempty"`
}

func (r *hubRecord) newer(than *hubRecord) bool {
	return than == nil || r.Epoch > than.Epoch || r.Epoch == than.Epoch && r.Rev > than.Rev
}

func (r *hubRecord) lists(hubID string) (recNeighbor, bool) {
	for _, n := range r.Neighbors {
		if n.HubID == hubID {
			return n, true
		}
	}
	return recNeighbor{}, false
}

type meshView struct {
	self string
	hubs map[string]*viewHub
}

type viewHub struct {
	rec       *hubRecord
	reachable bool
	hops      int    // mesh links from here (0: this hub)
	rtt       int    // ms over those links; -1 when a link's is unknown
	next      string // the direct peer the way there starts with ("" for this hub)
}

func (v *meshView) reachable(hubID string) bool {
	if v == nil {
		return false
	}
	vh := v.hubs[hubID]
	return vh != nil && vh.reachable
}

// next is the direct peer to send something for hubID through ("" when it cannot be reached, or is this hub).
func (v *meshView) next(hubID string) string {
	if v == nil || hubID == v.self {
		return ""
	}
	if vh := v.hubs[hubID]; vh != nil && vh.reachable {
		return vh.next
	}
	return ""
}

// rttTo is the round trip over the links to hubID in ms, -1 when unknown or unreachable.
func (v *meshView) rttTo(hubID string) int {
	if v == nil {
		return -1
	}
	if vh := v.hubs[hubID]; vh != nil && vh.reachable {
		return vh.rtt
	}
	return -1
}

// computeView works out from the records (self's own among them) how every hub can be reached.
func computeView(self string, recs map[string]*hubRecord) *meshView {
	v := &meshView{self: self, hubs: map[string]*viewHub{}}
	for id, r := range recs {
		v.hubs[id] = &viewHub{rec: r, hops: -1, rtt: -1}
	}
	if v.hubs[self] == nil {
		v.hubs[self] = &viewHub{rec: &hubRecord{HubID: self}}
	}
	v.routes(recs)
	return v
}

// routes finds, from self, the shortest way to every hub over links both ends list: fewest links, then the
// smallest round trip, then the smaller next hop.
func (v *meshView) routes(recs map[string]*hubRecord) {
	edge := func(a, b string) (int, bool) {
		ra, rb := recs[a], recs[b]
		if ra == nil || rb == nil {
			return 0, false
		}
		na, ok1 := ra.lists(b)
		nb, ok2 := rb.lists(a)
		if !ok1 || !ok2 {
			return 0, false
		}
		switch {
		case na.RTTMS > 0 && nb.RTTMS > 0:
			return (na.RTTMS + nb.RTTMS) / 2, true
		case na.RTTMS > 0:
			return na.RTTMS, true
		}
		return nb.RTTMS, true
	}
	type cost struct {
		hops, rtt int
		unknown   bool
		next      string
	}
	better := func(a, b cost) bool {
		if a.hops != b.hops {
			return a.hops < b.hops
		}
		if a.unknown != b.unknown {
			return !a.unknown
		}
		if a.rtt != b.rtt {
			return a.rtt < b.rtt
		}
		return a.next < b.next
	}
	dist := map[string]cost{v.self: {}}
	done := map[string]bool{}
	for {
		cur, found := "", false
		var cc cost
		for id, c := range dist {
			if done[id] {
				continue
			}
			if !found || better(c, cc) || (!better(cc, c) && id < cur) {
				cur, cc, found = id, c, true
			}
		}
		if !found {
			break
		}
		done[cur] = true
		r := recs[cur]
		if r == nil && cur != v.self {
			continue
		}
		var neighbors []recNeighbor
		if r != nil {
			neighbors = r.Neighbors
		}
		for _, n := range neighbors {
			rtt, ok := edge(cur, n.HubID)
			if !ok || done[n.HubID] {
				continue
			}
			nc := cost{hops: cc.hops + 1, rtt: cc.rtt + rtt, unknown: cc.unknown || rtt == 0, next: cc.next}
			if cur == v.self {
				nc.next = n.HubID
			}
			if old, seen := dist[n.HubID]; !seen || better(nc, old) {
				dist[n.HubID] = nc
			}
		}
	}
	for id, c := range dist {
		vh := v.hubs[id]
		if vh == nil {
			continue
		}
		vh.reachable, vh.hops, vh.next = true, c.hops, c.next
		vh.rtt = c.rtt
		if c.unknown && id != v.self {
			vh.rtt = -1
		}
	}
}

// reachableIDs is every hub that can be reached, this one included, sorted.
func (v *meshView) reachableIDs() []string {
	var out []string
	if v == nil {
		return out
	}
	for id, vh := range v.hubs {
		if vh.reachable {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func shortID(hubID string) string {
	if len(hubID) > 6 {
		return hubID[:6]
	}
	return hubID
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
