package devices

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// The manager's own list of computers (managerconf.MachinesKey in its manager.json; docs/assistant/managers.md "Each
// manager's computers"): a manager whose hub has not answered within ownListWait of its start connects them, the way
// the hub's last hand-over had them (its tailcat DERP map). Not when the hub was in a mesh group: which computers are
// this hub's to connect, and when, are the group's, so it waits for the hub. Every hand-over is written there
// (keepOwnList); the hub's, when it comes, is the list (Configure), and what the connections made meanwhile could not
// do without the hub is done then (afterOwnList).

// ownListWait is how long a starting manager waits for the hub's hand-over before it connects its own list: a hub that
// is up answers well within it (the built-in manager is started by its hub), so then nothing connects before it.
var ownListWait = 20 * time.Second

// startOwnList connects the machines of the manager's own list, the hub's hand-over not having come (Run).
func (d *Devices) startOwnList(ctx context.Context) {
	if d.opts.ConfigFile == "" {
		return
	}
	list, from, ok, err := managerconf.OwnMachines(d.opts.ConfigFile)
	if err != nil {
		d.logf("own list of machines: %v: waiting for the hub's", err)
		return
	}
	if !ok {
		return
	}
	if from.Mesh {
		d.logf("own list of machines: the hub was in a mesh group: waiting for it")
		return
	}
	d.mu.Lock()
	if d.got { // the hub's came meanwhile
		d.mu.Unlock()
		return
	}
	d.cfg = devicewire.Config{Machines: list, TailcatDERPMapURL: from.TailcatDERPMapURL}
	d.early = true
	d.mu.Unlock()
	d.logf("own list of machines: the hub has not answered: %d from %s", len(list), d.opts.ConfigFile)
	d.startMachines(ctx)
}

// afterOwnList is the hub's first hand-over after a start from the own list: a node that connected meanwhile is
// offered the hub's build, which was not known then (updateOnConnect). Its launchers and the hub's part of its
// connection follow BusUp, as on every handshake.
func (d *Devices) afterOwnList(ctx context.Context) {
	for _, m := range d.cfgMachines() {
		go m.updateOnConnect(ctx)
	}
}

// keepOwnList writes the hub's hand-over, as it is now, into the manager's own list (Configure).
func (d *Devices) keepOwnList() {
	if d.opts.ConfigFile == "" {
		return
	}
	d.ownMu.Lock()
	defer d.ownMu.Unlock()
	d.mu.Lock()
	cfg, got := d.cfg, d.got
	d.mu.Unlock()
	if !got || cfg.Machines == nil {
		return
	}
	from := managerconf.MachinesFromHub{TailcatDERPMapURL: cfg.TailcatDERPMapURL, HubID: cfg.HubID, Mesh: cfg.Mesh != nil}
	changed, skipped, replaced, err := managerconf.KeepMachines(d.opts.ConfigFile, cfg.Machines, from)
	var say string
	switch {
	case err != nil:
		say = "own list of machines: not kept in " + d.opts.ConfigFile + ": " + err.Error()
	case replaced != nil:
		say = "own list of machines: " + d.opts.ConfigFile + "'s did not read (" + replaced.Error() + "): the hub's written over it"
	case changed && skipped != nil:
		say = "own list of machines: kept in " + d.opts.ConfigFile + " without what does not read: " + skipped.Error()
	}
	if say != "" && say != d.ownSaid { // said once, not at every hand-over
		d.logf("%s", say)
	}
	d.ownSaid = say
}
