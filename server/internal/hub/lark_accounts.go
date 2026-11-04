package hub

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
)

// Several Feishu accounts on one lark-connector (docs/feishu.md「多账号」): a /api/v1/lark request names its account with
// ol_account, the inbox collects every signed-in account and each has its own realtime connection.
// Without ol_account a request is lark.account's, as before there were several.

// larkMux registers the /api/v1/lark routes: each handler finds the client of the account its request names with
// larkOf.
type larkMux struct {
	api *http.ServeMux
	h   *Hub
}

type larkClientKey struct{}

func (m larkMux) HandleFunc(pattern string, fn func(http.ResponseWriter, *http.Request)) {
	m.api.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		c := m.h.lark
		if s := r.URL.Query().Get("ol_account"); s != "" {
			id, ok := lark.ParseAccount(s)
			if !ok {
				writeErr(w, http.StatusBadRequest, errors.New("ol_account must be an lark-connector account number"))
				return
			}
			c = c.ForAccount(id)
		}
		fn(w, r.WithContext(context.WithValue(r.Context(), larkClientKey{}, c)))
	})
}

// larkOf is the lark-connector client of the account a /api/v1/lark request is for.
func (h *Hub) larkOf(r *http.Request) *lark.Client {
	if c, ok := r.Context().Value(larkClientKey{}).(*lark.Client); ok {
		return c
	}
	return h.lark
}

// larkAccountRoutes: GET /api/v1/lark/accounts, the accounts on the lark-connector and which one is lark.account.
func (h *Hub) larkAccountRoutes(api larkMux) {
	api.HandleFunc("GET /api/v1/lark/accounts", func(w http.ResponseWriter, r *http.Request) {
		list, err := h.lark.Accounts(r.Context())
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		h.larkAccounts.remember(list)
		writeJSON(w, map[string]any{"accounts": list, "default": h.lark.Account()})
	})
}

// larkAccountList is what the inbox and the realtime connections go by, read at most once a larkAccountsTTL.
type larkAccountList struct {
	mu   sync.Mutex
	list []lark.Account
	at   time.Time
}

const larkAccountsTTL = time.Minute

func (l *larkAccountList) remember(list []lark.Account) {
	l.mu.Lock()
	l.list, l.at = list, time.Now()
	l.mu.Unlock()
}

// larkSignedIn is the accounts to read from: every signed-in one, lark.account first. An lark-connector that cannot list
// them (an error) leaves lark.account alone, as before there were several.
func (h *Hub) larkSignedIn(ctx context.Context) []lark.Account {
	l := &h.larkAccounts
	l.mu.Lock()
	list, fresh := l.list, time.Since(l.at) < larkAccountsTTL
	l.mu.Unlock()
	if !fresh {
		read, err := h.lark.Accounts(ctx)
		if err == nil {
			l.remember(read)
			list = read
		} else if list == nil {
			list = []lark.Account{{ID: h.lark.Account(), SignedIn: true}}
		}
	}
	def := h.lark.Account()
	out := []lark.Account{{ID: def, SignedIn: true}}
	for _, a := range list {
		switch {
		case a.ID == def:
			out[0] = a
			out[0].SignedIn = true // lark.account is read even signed out: its errors say so, as before
		case a.SignedIn:
			out = append(out, a)
		}
	}
	return out
}

// larkAccountLabel is an item's source line: 飞书, and whose when more than one account is read.
func larkAccountLabel(a lark.Account, several bool) string {
	if !several || a.Name == "" {
		return "飞书"
	}
	return "飞书 · " + a.Name
}
