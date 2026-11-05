package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

// DialAt opens another WebSocket on the hub with a service's token: path (with query) instead of the bus itself, e.g.
// the manager's relay channel (devicewire.RelayPath). A refusal before the upgrade comes back with the body's error.
func DialAt(ctx context.Context, hub, token string, client *http.Client, path, query string) (*websocket.Conn, error) {
	base, err := url.Parse(strings.TrimRight(hub, "/") + "/")
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("bad hub url %q", hub)
	}
	u := base.JoinPath(strings.TrimPrefix(path, "/"))
	u.RawQuery = query
	ws, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: client,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			if resp.StatusCode == http.StatusUnauthorized {
				return nil, ErrRejected
			}
			var e struct {
				Error string `json:"error"`
			}
			if resp.Body != nil {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
				if json.Unmarshal(b, &e) == nil && e.Error != "" {
					return nil, fmt.Errorf("%s", e.Error)
				}
			}
		}
		return nil, err
	}
	return ws, nil
}
