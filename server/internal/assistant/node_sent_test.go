package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A node's lark.sent goes to the extensions' ledger with the machine of the connection it came in on, never a machine
// the params name; without extensions there is no ledger and the node hears so.
func TestNodeRequestLarkSent(t *testing.T) {
	s := newFleetTest(t).s // the nodes' requests are the fleet's
	ctx := context.Background()
	params := json.RawMessage(`{"method":"POST","path":"/conversations/oc_1/messages","machine_id":"laptop","let_through":{"kind":"conversation_always"}}`)

	if _, handled, err := s.NodeRequest(ctx, "mac", proto.MethodLarkSent, params); !handled || err == nil {
		t.Fatalf("without extensions: handled %v, err %v", handled, err)
	}

	ext := &guestExt{}
	s.SetExtensions(ext)
	out, handled, err := s.NodeRequest(ctx, "mac", proto.MethodLarkSent, params)
	if err != nil || !handled || out == nil {
		t.Fatalf("handled %v, out %v, err %v", handled, out, err)
	}
	if len(ext.sent) != 1 || !strings.HasPrefix(ext.sent[0], "mac {") || !strings.Contains(ext.sent[0], `"path":"/conversations/oc_1/messages"`) {
		t.Fatalf("the ledger was told %q", ext.sent)
	}
}
