package lark

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// Several Feishu accounts can be signed in on one lark-connector instance (its x-multi-account, GET /agent/v1/accounts):
// a request names one with the x-lark-connector-account header or the ol_account query. Persona's Client is one account;
// ForAccount derives the others (docs/feishu.md「多账号」).

// Account is one Feishu account signed in on the lark-connector instance.
type Account struct {
	ID          int    `json:"id"`
	Name        string `json:"name,omitempty"`
	Avatar      string `json:"avatar,omitempty"` // hub-relative for lark-connector media (MediaURL)
	Tenant      string `json:"tenant,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	SignedIn    bool   `json:"signed_in"`
	Primary     bool   `json:"primary,omitempty"` // lark-connector's account 1, the instance as it was before more were added
	Unread      int    `json:"unread"`
	Quarantined bool   `json:"quarantined,omitempty"` // the slot could not be opened; lark-connector offers to retry or remove it
}

// MaxAccount is the largest account number hub.json, the routes and lark-connector accept.
const MaxAccount = 999999999

// ParseAccount reads an account number as lark-connector's routes do: a positive decimal integer.
func ParseAccount(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxAccount || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// ForAccount is the client for another account on the same lark-connector instance: the same base URL and token, its
// own account header and caches. Asking for this client's own account (or zero) returns it.
func (c *Client) ForAccount(id int) *Client {
	root := c
	if c.root != nil {
		root = c.root
	}
	if id <= 0 {
		id = root.cfg.Account
	}
	if id == c.cfg.Account {
		return c
	}
	if id == root.cfg.Account {
		return root
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if s := root.accounts[id]; s != nil {
		return s
	}
	cfg := root.cfg
	cfg.Account = id
	s := New(cfg)
	s.root = root
	if root.accounts == nil {
		root.accounts = map[int]*Client{}
	}
	root.accounts[id] = s
	return s
}

// Accounts lists the accounts on the lark-connector instance in its order. An lark-connector from before it had several
// accounts has no /accounts route: then the list is this client's account alone, as the user it is signed in as.
func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	var out struct {
		Accounts []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			AvatarURL   string `json:"avatarUrl"`
			TenantName  string `json:"tenantName"`
			UserID      string `json:"userId"`
			SignedIn    bool   `json:"signedIn"`
			Primary     bool   `json:"primary"`
			UnreadCount int    `json:"unreadCount"`
			Quarantined bool   `json:"quarantined"`
		} `json:"accounts"`
	}
	err := c.do(ctx, "GET", "/accounts", nil, nil, &out)
	var de *doError
	if errors.As(err, &de) && de.Status == http.StatusNotFound {
		return []Account{{ID: c.Account(), SignedIn: true, Primary: c.Account() == 1}}, nil
	}
	if err != nil {
		return nil, err
	}
	list := make([]Account, 0, len(out.Accounts))
	for _, a := range out.Accounts {
		id, ok := ParseAccount(a.ID)
		if !ok {
			continue
		}
		list = append(list, Account{ID: id, Name: a.Name, Avatar: MediaURL(a.AvatarURL), Tenant: a.TenantName, UserID: a.UserID,
			SignedIn: a.SignedIn && !a.Quarantined, Primary: a.Primary, Unread: a.UnreadCount, Quarantined: a.Quarantined})
	}
	return list, nil
}
