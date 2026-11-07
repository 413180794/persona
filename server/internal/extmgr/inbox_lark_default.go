package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
)

// The inbox's default Feishu account (#5098): the Feishu extension manages the accounts, so the default is the one
// its messaging source "lark" answers to GET accounts (docs/extensions/extension-messaging.md), not hub.json's lark.account.
// The inbox's Feishu items of the default account go without ol_account, the others' carry theirs.

// larkDefaultWithin is how long inbox.config waits for the extension's account list.
const larkDefaultWithin = 2 * time.Second

// larkDefaultMost is the largest account list read.
const larkDefaultMost = 1 << 20

// larkDefault is what the manager knows of the extension's default account.
type larkDefault struct {
	last atomic.Int64 // the default the extension last answered; zero until it has
	// read stands in for asking the extension (tests); nil asks it.
	read func(context.Context) (int, bool)
}

// inboxLarkAccount is the default Feishu account the inbox goes by: the extension's; while it can't answer (not
// running, restarting) the one it answered last; hub.json's lark.account when it never answered since the start.
func (h *Manager) inboxLarkAccount(ctx context.Context) int {
	read := h.larkDefault.read
	if read == nil {
		read = h.readLarkDefault
	}
	if a, ok := read(ctx); ok {
		h.larkDefault.last.Store(int64(a))
		return a
	}
	if a := h.larkDefault.last.Load(); a > 0 {
		return int(a)
	}
	if h.cfg == nil {
		return 0
	}
	return h.cfg.Lark.Account
}

// readLarkDefault asks the running extension that has the messaging source "lark" for its default account.
func (h *Manager) readLarkDefault(ctx context.Context) (int, bool) {
	hp := h.extensions.messagingSource(connector.Lark)
	if hp == nil || hp.client == nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, larkDefaultWithin)
	defer cancel()
	end, err := hp.proc.BeginHTTP(ctx)
	if err != nil {
		return 0, false
	}
	defer end()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://plugin/api/v1/messaging/"+connector.Lark+"/accounts", nil)
	if err != nil {
		return 0, false
	}
	res, err := hp.client.Do(req)
	if err != nil {
		return 0, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, false
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, larkDefaultMost))
	if err != nil {
		return 0, false
	}
	return larkDefaultOf(body)
}

// larkDefaultOf is the default account of a GET accounts answer: a gateway account number (the inbox's items say
// theirs as one); false when the answer names none.
func larkDefaultOf(body []byte) (int, bool) {
	var accounts wire.MessagingAccounts
	if json.Unmarshal(body, &accounts) != nil {
		return 0, false
	}
	a, err := strconv.Atoi(accounts.Default)
	if err != nil || a < 1 || a > lark.MaxAccount {
		return 0, false
	}
	return a, true
}
