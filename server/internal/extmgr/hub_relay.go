package extmgr

// How a manager's extensions reach the hub's API (docs/assistant/managers.md "Extensions and the hub's API"). A relayed manager
// (relayed) serves them the hub's API on its own loopback (extension/hubrelay) and passes each request on with its own
// client and its proof that it came through it, and it hands them tokens it makes itself: of the scopes each was
// granted, or of every scope for one that asked for the hub's full token the old way (hub_api, manager.json's
// extensions). The hub checks such a token as any extension's: it asks the managers whose it is
// (extensions.authorize), and the one that knows it is the one the extension acts as. The built-in manager is not
// relayed yet: its extensions reach the hub on this machine as hub.json says, and the old way get the hub's own
// token, as they always did.

import (
	"cmp"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension/hubrelay"
)

// hubRelay is the manager's relay to the hub, started the first time an extension needs it.
type hubRelay struct {
	once sync.Once
	url  string // http://127.0.0.1:<port>
	err  error
}

// builtin says whether this is the built-in manager.
func (h *Manager) builtin() bool {
	return h.opts.ManagerID == "" || h.opts.ManagerID == assistantwire.AssistantID
}

// relayed says whether this manager's extensions reach the hub through its relay, with its tokens only: every manager
// but the built-in one for now. The built-in one becomes relayed here once it is a service of its own (no hub.json, no
// hub token: step 3), with Options.HubURL and HubProofKey set as for any other.
func (h *Manager) relayed() bool { return !h.builtin() }

// hubURL is the hub's API as this manager's extensions reach it: the relay for a relayed manager, else the hub on
// this machine.
func (ps *extensions) hubURL() (string, error) {
	if !ps.h.relayed() {
		return hubLocalURL(ps.h.cfg.Listen), nil
	}
	r := &ps.hubRelay
	r.once.Do(func() {
		if r.url, r.err = ps.startHubRelay(); r.err != nil {
			r.err = fmt.Errorf("the hub's API for the extensions: %w", r.err)
			ps.logf("%v", r.err)
		}
	})
	return r.url, r.err
}

// startHubRelay serves the relay until the manager stops.
func (ps *extensions) startHubRelay() (string, error) {
	var transport http.RoundTripper
	if c := ps.h.opts.HubClient; c != nil {
		transport = c.Transport
	}
	ctx := ps.ctx
	if ctx == nil {
		ctx = ps.h.ctx
	}
	if ctx == nil {
		return "", fmt.Errorf("not started")
	}
	u, err := hubrelay.Start(ctx, hubrelay.Options{Hub: ps.h.opts.HubURL, Transport: transport,
		Manager: cmp.Or(ps.h.opts.ManagerID, assistantwire.AssistantID), ProofKey: ps.h.opts.HubProofKey,
		Knows: func(tok string) bool {
			_, ok := ps.h.extTokens.lookup(tok)
			return ok && strings.HasPrefix(tok, extTokenPrefix)
		},
		Ready:      ps.h.opts.Connected, // off the bus, the hub could not tell whose its extensions' tokens are
		Connection: ps.h.opts.Connection,
		Logf:       ps.logf})
	if err != nil {
		return "", err
	}
	ps.logf("the hub's API for this manager's extensions is %s", u)
	return u, nil
}
