package hub

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// The computers the center drives and the ways to each (docs/plan-hub-center.md 5, docs/mesh.md「多条路」). A way —
// a path — is a hub.json entry of this hub (local, service, link, ssh, tailcat) or one of a member hub's (the center
// opens the node's stream through that hub, peer_node.go). Paths that turn out to reach the same computer (the same
// machine_uid in node.info) are one machine: listed once, connected through one path at a time (a resident node takes
// one attach and drops the one before). Which: this machine's own > a direct path > through a member, and the
// smaller round trip among equals. Sticky: the path in use stays while it works; a broken one is left at once for the
// next; a better one must stay better for mesh.path_switch_seconds before the switch, which connects the new one
// before letting the old one go.

const (
	pathClassLocal  = 0
	pathClassDirect = 1
	pathClassHub    = 2
	viaHub          = "hub" // a path through a member hub
	kindHub         = "hub" // the kind of a machine only a member has in hub.json
)

// pathCheckEvery: how often the path in use is compared with the others (a var: tests shorten it).
var pathCheckEvery = 2 * time.Second

// nodePath is one way to a computer's node.
type nodePath struct {
	key string   // "cfg:<id>" for this hub's hub.json entry, "hub:<hub_id>/<id>" for a member's
	via string   // the entry's kind (local | service | link | ssh | tailcat), or "hub"
	src *Machine // cfg: the entry whose way it is (its transports, its link, its connector)
	hub string   // hub: the member
	mid string   // hub: the computer's id in the member's hub.json
	rec recMachine
}

func (p *nodePath) class() int {
	switch {
	case p.via == viaHub:
		return pathClassHub
	case p.via == "local":
		return pathClassLocal
	}
	return pathClassDirect
}

// pathHealth is how a path did lately.
type pathHealth struct {
	failUntil time.Time // not tried again before this (backoff after a failure)
	fails     int
	err       string
	okAt      time.Time // last connected through it
	rtt       int       // ms: the node's round trip the last time it was in use
}

// pathInfo is a path as the admin page and the machine list show it (plan 5「paths」).
type pathInfo struct {
	Via    string `json:"via"`
	HubID  string `json:"hub_id"`
	ID     string `json:"id,omitempty"` // the hub.json entry it comes from (there)
	OK     bool   `json:"ok"`
	RTTMS  int    `json:"rtt_ms,omitempty"`
	Active bool   `json:"active,omitempty"`
	Error  string `json:"error,omitempty"`
}

type fleet struct {
	mu       sync.Mutex
	derived  map[string]*Machine   // group key -> a machine only members have
	absorbed map[*Machine]*Machine // a hub.json entry whose computer another entry has -> that one
	paths    map[*Machine][]*nodePath
	ids      map[string]string // group key -> the id given to a derived machine (setting "mesh.machine_ids")
	uids     map[string]string // path key -> the machine_uid seen through it (setting "mesh.path_uids")
	health   map[string]*pathHealth
	cancel   map[*Machine]context.CancelFunc // derived machines' run loops
	extra    []*Machine                      // hub.json entries added while the hub runs (admin page)
	changed  chan struct{}                   // closed when a regroup changed something
	order    []*Machine                      // the machines listed: hub.json's (not absorbed), then derived by id
}

func (f *fleet) init() {
	if f.paths == nil {
		f.derived, f.absorbed, f.paths = map[string]*Machine{}, map[*Machine]*Machine{}, map[*Machine][]*nodePath{}
		f.ids, f.uids, f.health = map[string]string{}, map[string]string{}, map[string]*pathHealth{}
		f.cancel, f.changed = map[*Machine]context.CancelFunc{}, make(chan struct{})
	}
}

// machinesNow is every machine the apps see, in order: hub.json's (but entries for a computer another entry has),
// then the members' computers. Before the first regroup (and in unit tests) it is hub.json's.
func (h *Hub) machinesNow() []*Machine {
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order == nil {
		return h.cfgMachinesLocked()
	}
	return append([]*Machine(nil), f.order...)
}

// fleetAll is every machine object with a run loop: hub.json's, absorbed ones included, and the derived ones.
func (h *Hub) fleetAll() []*Machine {
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	out := h.cfgMachinesLocked()
	for _, m := range f.derived {
		out = append(out, m)
	}
	return out
}

// machineByID is the machine an id the apps use stands for: a hub.json entry (or the entry that has its computer),
// or a member's computer.
func (h *Hub) machineByID(id string) *Machine {
	if id == "" {
		return nil
	}
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	if m := h.cfgMachineLocked(id); m != nil {
		if to := f.absorbed[m]; to != nil {
			return to
		}
		return m
	}
	for _, m := range f.derived {
		if m.cfg.ID == id {
			return m
		}
	}
	return nil
}

// localMachine is machineByID (the name the request handlers use).
func (h *Hub) localMachine(id string) *Machine { return h.machineByID(id) }

// pathsOf is a machine's paths, best first by kind (the choice also looks at health and round trips).
func (h *Hub) pathsOf(m *Machine) []*nodePath {
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	if ps, ok := f.paths[m]; ok {
		return append([]*nodePath(nil), ps...)
	}
	if !m.derived {
		return []*nodePath{cfgPath(m)}
	}
	return nil
}

func cfgPath(m *Machine) *nodePath {
	return &nodePath{key: "cfg:" + m.cfg.ID, via: kindOf(m.cfg), src: m}
}

func kindOf(c MachineConfig) string {
	if c.Kind == "" {
		return "ssh"
	}
	return c.Kind
}

// cfgMachines is every hub.json entry: those read at start and those the admin page added since.
func (h *Hub) cfgMachines() []*Machine {
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	return h.cfgMachinesLocked()
}

func (h *Hub) cfgMachinesLocked() []*Machine {
	out := append([]*Machine(nil), h.machines...)
	return append(out, h.fleet.extra...)
}

// cfgMachine is the hub.json entry with this id (not the machine an app's id leads to: see machineByID).
func (h *Hub) cfgMachine(id string) *Machine {
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	return h.cfgMachineLocked(id)
}

func (h *Hub) cfgMachineLocked(id string) *Machine {
	if m := h.byID[id]; m != nil {
		return m
	}
	for _, m := range h.fleet.extra {
		if m.cfg.ID == id {
			return m
		}
	}
	return nil
}

// addMachineLive starts a hub.json entry the admin page just added (nil when the hub is not running: it is read at
// the next start).
func (h *Hub) addMachineLive(mc MachineConfig) *Machine {
	ctx := h.meshCtx()
	if ctx == nil || h.cfgMachine(mc.ID) != nil {
		return nil
	}
	m := newMachine(h, mc)
	f := &h.fleet
	f.mu.Lock()
	f.init()
	f.extra = append(f.extra, m)
	f.mu.Unlock()
	h.nodeWorkers.Add(1)
	go func() {
		defer h.nodeWorkers.Done()
		m.run(ctx)
	}()
	h.fleetRegroup()
	h.meshTouch()
	h.logf("[%s] added (%s): connecting", mc.ID, kindOf(mc))
	return m
}

// meshCtx is the hub's run context once the mesh started (nil before, and in unit tests without one).
func (h *Hub) meshCtx() context.Context {
	ms := &h.mesh
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.ctx
}

// fleetChanged returns a channel closed at the next change of the machines or their paths.
func (h *Hub) fleetChanged() <-chan struct{} {
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	return f.changed
}

// loadPathUIDs reads which computer each path reached last time, and the ids given to members' computers.
func (h *Hub) loadPathUIDs(ctx context.Context) {
	f := &h.fleet
	f.mu.Lock()
	f.init()
	f.mu.Unlock()
	if h.store == nil {
		return
	}
	var uids, ids map[string]string
	if _, err := h.store.Setting(ctx, "mesh.path_uids", &uids); err != nil {
		h.logf("mesh: load path uids: %v", err)
	}
	if _, err := h.store.Setting(ctx, "mesh.machine_ids", &ids); err != nil {
		h.logf("mesh: load machine ids: %v", err)
	}
	f.mu.Lock()
	for k, v := range uids {
		f.uids[k] = v
	}
	for k, v := range ids {
		f.ids[k] = v
	}
	f.mu.Unlock()
}

// learnUID keeps the computer a path reached; when it is news, the machines are grouped again.
func (h *Hub) learnUID(p *nodePath, uid string) {
	if uid == "" || p == nil {
		return
	}
	f := &h.fleet
	f.mu.Lock()
	f.init()
	if f.uids[p.key] == uid {
		f.mu.Unlock()
		return
	}
	f.uids[p.key] = uid
	save := make(map[string]string, len(f.uids))
	for k, v := range f.uids {
		save[k] = v
	}
	f.mu.Unlock()
	if h.store != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.store.PutSetting(ctx, "mesh.path_uids", save); err != nil {
				h.logf("mesh: save path uids: %v", err)
			}
		}()
	}
	h.fleetRegroup()
}

// memberRecords are the records of the hubs whose computers the center reaches through them: the other hubs that
// take this hub for their center, not seen for too long excluded.
func (h *Hub) memberRecords() []*hubRecord {
	self := h.hubID()
	ms := &h.mesh
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if !ms.running {
		return nil
	}
	var out []*hubRecord
	for id, r := range ms.records {
		if id == self || r.Center != self || time.Since(ms.seenAt[id]) > meshRecordKeep {
			continue
		}
		if _, paired := ms.peers[id]; !paired && !h.meshView().reachable(id) {
			continue // left the group (no longer paired with anyone here)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HubID < out[j].HubID })
	return out
}

// fleetRegroup works out the machines and their paths again: hub.json's entries, and (on the center) the members'
// computers, grouped by the computer each path reached.
func (h *Hub) fleetRegroup() {
	center := h.isCenter()
	var members []*hubRecord
	if center {
		members = h.memberRecords()
	}
	f := &h.fleet
	f.mu.Lock()
	f.init()

	cfgs := h.cfgMachinesLocked()
	var all []*nodePath
	for _, m := range cfgs {
		all = append(all, cfgPath(m))
	}
	for _, r := range members {
		for _, rm := range r.Machines {
			all = append(all, &nodePath{key: "hub:" + r.HubID + "/" + rm.ID, via: viaHub, hub: r.HubID, mid: rm.ID, rec: rm})
		}
	}
	// Group by computer: one group per uid, a path whose computer is not known yet alone.
	type group struct {
		key   string
		paths []*nodePath
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, p := range all {
		k := "p:" + p.key
		if uid := f.uids[p.key]; uid != "" {
			k = "u:" + uid
		}
		g := byKey[k]
		if g == nil {
			g = &group{key: k}
			byKey[k] = g
			groups = append(groups, g)
		}
		g.paths = append(g.paths, p)
	}

	newPaths := map[*Machine][]*nodePath{}
	newAbsorbed := map[*Machine]*Machine{}
	newDerived := map[string]*Machine{}
	taken := map[string]bool{}
	for _, m := range cfgs {
		taken[m.cfg.ID] = true
	}
	var listed []*Machine
	var pendingDerived []*group
	for _, g := range groups {
		var owner *Machine
		for _, p := range g.paths {
			if p.src == nil {
				continue
			}
			if owner == nil {
				owner = p.src
			} else {
				newAbsorbed[p.src] = owner
			}
		}
		if owner == nil {
			pendingDerived = append(pendingDerived, g)
			continue
		}
		newPaths[owner] = orderPaths(g.paths)
		listed = append(listed, owner)
	}
	// The members' computers: the machine made earlier for any of the group's paths is kept (its id with it).
	prevOf := map[string]*Machine{}
	for _, m := range f.derived {
		for _, p := range f.paths[m] {
			prevOf[p.key] = m
		}
	}
	for _, m := range f.derived {
		taken[m.cfg.ID] = true // ids in use stay with their machines
	}
	used := map[*Machine]bool{}
	var derivedList []*Machine
	for _, g := range pendingDerived {
		var m *Machine
		for _, p := range g.paths {
			if prev := prevOf[p.key]; prev != nil && !used[prev] {
				m = prev
				break
			}
		}
		ps := orderPaths(g.paths)
		if m == nil {
			id := f.ids[g.key]
			if id == "" || taken[id] && !derivedHas(f.derived, id) {
				id = derivedID(ps[0], taken, h.hubNameOf(ps[0].hub))
			}
			taken[id] = true
			cfg := ps[0].rec.Cfg
			cfg.ID, cfg.Kind = id, kindHub
			if cfg.Name == "" {
				cfg.Name = ps[0].mid
			}
			m = newMachine(h, cfg)
			m.derived = true
		}
		used[m] = true
		if f.ids[g.key] != m.cfg.ID {
			f.ids[g.key] = m.cfg.ID
		}
		newDerived[g.key] = m
		newPaths[m] = ps
		derivedList = append(derivedList, m)
	}
	sort.Slice(derivedList, func(i, j int) bool { return derivedList[i].cfg.ID < derivedList[j].cfg.ID })

	// What changed: machines gone, machines new, paths moved.
	var gone, started []*Machine
	for _, m := range f.derived {
		if !used[m] {
			gone = append(gone, m)
		}
	}
	for _, m := range newDerived {
		if f.cancel[m] == nil {
			started = append(started, m)
		}
	}
	changed := len(gone) > 0 || len(started) > 0 || len(newAbsorbed) != len(f.absorbed) || !samePaths(f.paths, newPaths)
	for m, to := range newAbsorbed {
		if f.absorbed[m] != to {
			changed = true
		}
	}
	f.derived, f.absorbed, f.paths = newDerived, newAbsorbed, newPaths
	f.order = append(listed, derivedList...)
	ctx := h.meshCtx()
	cancels := map[*Machine]context.CancelFunc{}
	for _, m := range gone {
		cancels[m] = f.cancel[m]
		delete(f.cancel, m)
	}
	for _, m := range started {
		if ctx == nil {
			continue
		}
		mctx, cancel := context.WithCancel(ctx)
		f.cancel[m] = cancel
		h.nodeWorkers.Add(1)
		go func() {
			defer h.nodeWorkers.Done()
			m.run(mctx)
		}()
	}
	ids := make(map[string]string, len(f.ids))
	for k, v := range f.ids {
		if newDerived[k] != nil {
			ids[k] = v
		}
	}
	f.ids = ids
	var wake []*Machine
	if changed {
		close(f.changed)
		f.changed = make(chan struct{})
		for m := range newPaths {
			wake = append(wake, m)
		}
		wake = append(wake, cfgs...)
	}
	f.mu.Unlock()

	for m, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
		h.logf("[%s] no longer reachable through any hub of the group: removed", m.cfg.ID)
	}
	for _, m := range started {
		h.logf("[%s] a member's computer: reached through %s", m.cfg.ID, h.hubNameOf(h.pathsOf(m)[0].hub))
	}
	if !changed {
		return
	}
	for _, m := range wake {
		m.wake()
	}
	if h.store != nil {
		go func() {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.store.PutSetting(sctx, "mesh.machine_ids", ids)
		}()
	}
	h.broadcastMachines()
}

func derivedHas(d map[string]*Machine, id string) bool {
	for _, m := range d {
		if m.cfg.ID == id {
			return true
		}
	}
	return false
}

// derivedID is the id a member's computer gets: its id in that member's hub.json, unless the center (or another
// member) has it already; then @<the member's name>, then the member's hub_id too, then a number.
func derivedID(p *nodePath, taken map[string]bool, hubName string) string {
	base := p.mid
	if !taken[base] {
		return base
	}
	suffix := hubSlug(hubName, p.hub)
	id := base + "@" + suffix
	if taken[id] {
		id = base + "@" + suffix + "-" + shortID(p.hub)
	}
	for n := 2; taken[id]; n++ {
		id = base + "@" + suffix + "-" + shortID(p.hub) + "-" + itoa(n)
	}
	return id
}

// orderPaths: hub.json entries in their order, then members' by hub_id and id.
func orderPaths(ps []*nodePath) []*nodePath {
	out := append([]*nodePath(nil), ps...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.src != nil) != (b.src != nil) {
			return a.src != nil
		}
		if a.src != nil {
			return false // hub.json order
		}
		if a.hub != b.hub {
			return a.hub < b.hub
		}
		return a.mid < b.mid
	})
	return out
}

func samePaths(a, b map[*Machine][]*nodePath) bool {
	if len(a) != len(b) {
		return false
	}
	for m, pa := range a {
		pb, ok := b[m]
		if !ok || len(pa) != len(pb) {
			return false
		}
		for i := range pa {
			if pa[i].key != pb[i].key || pa[i].rec.Avail != pb[i].rec.Avail {
				return false
			}
		}
	}
	return true
}

// wakeOwnerOf wakes the machine a hub.json entry's path belongs to (the entry itself, or the one that has its computer).
func (h *Hub) wakeOwnerOf(m *Machine) {
	f := &h.fleet
	f.mu.Lock()
	to := f.absorbed[m]
	f.mu.Unlock()
	if to != nil {
		to.wake()
	}
}

// parkReason says why a machine's run loop does not connect now ("" when it should): a member leaves its computers to
// the center; an entry whose computer another entry has is reached through that one.
func (h *Hub) parkReason(m *Machine) string {
	if h.isMember() {
		return "由中心 hub「" + h.hubNameOf(h.centerID()) + "」连着"
	}
	f := &h.fleet
	f.mu.Lock()
	defer f.mu.Unlock()
	if to := f.absorbed[m]; to != nil {
		return "和「" + to.cfg.Name + "」是同一台电脑"
	}
	if m.derived {
		if _, ok := f.paths[m]; !ok {
			return "removed"
		}
	}
	return ""
}

// pathState is what is known of a path now: whether it can be tried, its round trip (ms, 0 unknown).
type pathState struct {
	avail     bool
	rtt       int
	failUntil time.Time
	err       string
}

func (h *Hub) pathState(p *nodePath) pathState {
	f := &h.fleet
	f.mu.Lock()
	var hl pathHealth
	if x := f.health[p.key]; x != nil {
		hl = *x
	}
	f.mu.Unlock()
	st := pathState{failUntil: hl.failUntil, err: hl.err, rtt: hl.rtt}
	switch p.via {
	case viaHub:
		v := h.meshView()
		st.avail = v.reachable(p.hub) && p.rec.Avail
		if !v.reachable(p.hub) {
			st.err = "「" + h.hubNameOf(p.hub) + "」现在连不上"
		}
		if hr := v.rttTo(p.hub); hr >= 0 && p.rec.RTTMS > 0 {
			st.rtt = hr + p.rec.RTTMS
		}
	default:
		avail, rtt := p.src.reachableHere()
		st.avail = avail
		if rtt > 0 {
			st.rtt = rtt
		}
		if !avail {
			if why := p.src.unavailableWhy(); why != "" {
				st.err = why // what keeps it from being tried says more than how the last try ended
			}
		}
	}
	return st
}

// pathBefore orders two available paths: class, then a known round trip before an unknown one, then the smaller.
func pathBefore(a *nodePath, as pathState, b *nodePath, bs pathState) bool {
	if a.class() != b.class() {
		return a.class() < b.class()
	}
	if (as.rtt > 0) != (bs.rtt > 0) {
		return as.rtt > 0
	}
	return as.rtt < bs.rtt
}

// clearlyBetter: q is worth leaving p for — a better kind of path, or the same kind with a clearly smaller round trip.
func clearlyBetter(q *nodePath, qs pathState, p *nodePath, ps pathState) bool {
	if q.class() != p.class() {
		return q.class() < p.class()
	}
	return qs.rtt > 0 && ps.rtt > 0 && qs.rtt*10 < ps.rtt*7 && ps.rtt-qs.rtt >= 5
}

// choosePath is the path a machine should connect through now; nil and how long to wait when none can be tried.
func (h *Hub) choosePath(m *Machine) (*nodePath, time.Duration) {
	var best *nodePath
	var bestSt pathState
	wait := time.Duration(0)
	now := time.Now()
	for _, p := range h.pathsOf(m) {
		st := h.pathState(p)
		if !st.avail {
			continue
		}
		if now.Before(st.failUntil) {
			if d := st.failUntil.Sub(now); wait == 0 || d < wait {
				wait = d
			}
			continue
		}
		if best == nil || pathBefore(p, st, best, bestSt) {
			best, bestSt = p, st
		}
	}
	return best, wait
}

// betterPath is a path clearly better than the one in use that has stayed so for mesh.path_switch_seconds (since
// tracks when each became better); nil for none.
func (h *Hub) betterPath(m *Machine, cur *nodePath, since map[string]time.Time) *nodePath {
	curSt := h.pathState(cur)
	now := time.Now()
	var pick *nodePath
	var pickSt pathState
	seen := map[string]bool{}
	for _, p := range h.pathsOf(m) {
		if p.key == cur.key {
			continue
		}
		st := h.pathState(p)
		if !st.avail || now.Before(st.failUntil) || !clearlyBetter(p, st, cur, curSt) {
			continue
		}
		seen[p.key] = true
		if since[p.key].IsZero() {
			since[p.key] = now
		}
		if now.Sub(since[p.key]) < h.cfg.Mesh.pathSwitch() {
			continue
		}
		if pick == nil || pathBefore(p, st, pick, pickSt) {
			pick, pickSt = p, st
		}
	}
	for k := range since {
		if !seen[k] {
			delete(since, k)
		}
	}
	return pick
}

// pathEnded records how a connection through p went: a path that held for a while is fine again at once; one that
// failed waits, longer each time in a row (2 s doubling to a minute).
func (h *Hub) pathEnded(p *nodePath, online bool, err error, held time.Duration, planned bool) {
	f := &h.fleet
	f.mu.Lock()
	f.init()
	hl := f.health[p.key]
	if hl == nil {
		hl = &pathHealth{}
		f.health[p.key] = hl
	}
	switch {
	case planned:
		hl.fails, hl.failUntil, hl.err = 0, time.Time{}, ""
	case online && held > time.Minute:
		hl.fails = 0
		hl.failUntil = time.Now().Add(2 * time.Second)
	default:
		hl.fails = min(hl.fails+1, 6)
		hl.failUntil = time.Now().Add(min(time.Second<<hl.fails, time.Minute))
	}
	if err != nil && !planned {
		hl.err = err.Error()
	}
	f.mu.Unlock()
}

// pathConnected records that p reached the node.
func (h *Hub) pathConnected(p *nodePath) {
	f := &h.fleet
	f.mu.Lock()
	f.init()
	hl := f.health[p.key]
	if hl == nil {
		hl = &pathHealth{}
		f.health[p.key] = hl
	}
	hl.okAt, hl.err = time.Now(), ""
	f.mu.Unlock()
}

// pathRTT records the node's round trip through p (the path in use, measureNodes).
func (h *Hub) pathRTT(p *nodePath, rtt int) {
	f := &h.fleet
	f.mu.Lock()
	f.init()
	hl := f.health[p.key]
	if hl == nil {
		hl = &pathHealth{}
		f.health[p.key] = hl
	}
	if hl.rtt == 0 || abs(rtt-hl.rtt) > max(2, hl.rtt/5) { // noise does not count as a change
		hl.rtt = rtt
	}
	f.mu.Unlock()
}

// pathInfos is a machine's paths for the admin page and the machine list.
func (h *Hub) pathInfos(m *Machine) []pathInfo {
	active := m.activePathKey()
	self := h.hubID()
	var out []pathInfo
	for _, p := range h.pathsOf(m) {
		st := h.pathState(p)
		pi := pathInfo{Via: p.via, HubID: self, OK: st.avail && time.Now().After(st.failUntil), RTTMS: st.rtt,
			Active: p.key == active, Error: st.err}
		if p.via == viaHub {
			pi.HubID, pi.ID = p.hub, p.mid
		} else {
			pi.ID = p.src.cfg.ID
			if pi.Active {
				if t := m.transportNow(); t != "" {
					pi.Via = t // ssh that fell back to tailcat
				}
			}
		}
		if pi.Active {
			pi.OK, pi.Error = true, ""
		}
		out = append(out, pi)
	}
	return out
}

// measureNodes measures the round trip to every computer this hub drives, over the path in use.
func (h *Hub) measureNodes(ctx context.Context) {
	for _, m := range h.machinesNow() {
		p := m.activePath()
		if p == nil || !m.isOnline() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		start := time.Now()
		err := m.call(cctx, "ping", map[string]any{}, nil)
		cancel()
		if err == nil {
			h.pathRTT(p, max(1, int(time.Since(start).Milliseconds())))
		}
	}
}

// machineList is the machines the apps see, each with the hub it is reached through and its paths.
func (h *Hub) machineList() []proto.Machine {
	self, name := h.hubID(), h.hubName()
	list := h.machinesNow()
	out := make([]proto.Machine, 0, len(list))
	for _, m := range list {
		s := m.snapshot()
		s.HubID, s.HubName = self, name
		f := &h.fleet
		f.mu.Lock()
		for _, p := range f.paths[m] {
			if uid := f.uids[p.key]; uid != "" {
				s.MachineUID = uid
				break
			}
		}
		f.mu.Unlock()
		if uid := m.nodeUID(); uid != "" {
			s.MachineUID = uid
		}
		infos := h.pathInfos(m)
		route := &proto.MachineRoute{Via: kindOf(m.cfg)}
		if m.derived {
			route.Via = viaHub
		}
		for i, pi := range infos {
			if pi.Active || i == 0 && !s.Online {
				route = &proto.MachineRoute{Via: pi.Via}
				if pi.Via == viaHub {
					route.Hops = 1
					s.HubID, s.HubName = pi.HubID, h.hubNameOf(pi.HubID)
				}
				if s.Online && pi.RTTMS > 0 {
					rtt := pi.RTTMS
					route.RTTMS = &rtt
				}
				if pi.Active {
					break
				}
			}
		}
		s.Route = route
		if len(infos) > 1 || m.derived {
			for _, pi := range infos {
				s.Paths = append(s.Paths, proto.MachinePath{Via: pi.Via, HubID: pi.HubID, OK: pi.OK, RTTMS: pi.RTTMS, Active: pi.Active})
			}
		}
		out = append(out, s)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
