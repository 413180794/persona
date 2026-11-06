package hub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

// HTTP send failures and the backlog WebSocket use the same localized serializer without guessing delivery.
func TestRPCErrorBytesLocale(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		for _, tc := range []struct {
			err error
			key string
		}{
			{context.DeadlineExceeded, "rpc-errors.timeout"}, {rpc.ErrClosed, "rpc-errors.unreachable"},
		} {
			status, raw := callErrBytes(lang, tc.err)
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if status != 502 || body["error"] != i18n.T(lang, tc.key) || body["detail"] != tc.err.Error() || body["delivered"] != nil {
				t.Fatalf("%s: %d %v", lang, status, body)
			}
		}
	}
}
