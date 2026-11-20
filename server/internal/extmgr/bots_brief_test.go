package extmgr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/feishubot"
)

type briefDigestConnector struct {
	toolConnector
	calls int
}

func (c *briefDigestConnector) Call(_ context.Context, name string, args json.RawMessage) (string, error) {
	c.calls++
	return name + " " + string(args), nil
}

func TestLarkBriefReadUsesEnabledReadOnlyDigest(t *testing.T) {
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	c := &briefDigestConnector{toolConnector: toolConnector{id: "larkcontext", tools: []string{"larkcontext_digest"}}}
	if err := h.connectors.Add(c); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"day":"2030-11-20","slot":"morning","include_fyi_items":true}`)
	if got, err := h.ReadLarkBriefDigest(t.Context(), args); err != nil || got != "larkcontext_digest "+string(args) {
		t.Fatalf("expected Lark Context's digest: %q, %v", got, err)
	}
	if err := h.connectors.SetEnabled("larkcontext", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ReadLarkBriefDigest(t.Context(), args); err == nil || c.calls != 1 {
		t.Fatalf("disabled connector was called: %d, %v", c.calls, err)
	}
}

func TestLarkBriefPostUsesBoundDMAndOneUUID(t *testing.T) {
	w := newBotWorld(t)
	p := assistantwire.FeishuBriefPostParams{
		Owner: "ou_me", ExpiresAt: time.Now().Add(time.Minute), UUID: "brief-test",
		Card: feishubot.NewCard("Scheduled digest", feishubot.Blue, "").Markdown("test digest").JSON(),
	}
	if err := w.feishuErr(assistantwire.FeishuBriefPost, p); err == nil {
		t.Fatal("unpaired bot accepted the delivery")
	}
	w.pair(t)
	_, set := w.h.botAPI()
	if set.Features.Pushes {
		t.Fatal("test requires pushes disabled")
	}
	p.PairedAt = set.Owner.At
	out := w.feishu(t, assistantwire.FeishuBriefPost, p)
	if id, _ := out["message_id"].(string); id == "" {
		t.Fatalf("transport returned no message ID: %v", out)
	}
	w.api.find(t, "POST /open-apis/im/v1/messages ", `"receive_id":"oc_p2p"`, `"uuid":"brief-test"`, "test digest")

	for _, key := range []string{"brief-repaired", "brief-unknown-owner", "brief-expired"} {
		probe := p
		probe.UUID = key
		switch key {
		case "brief-repaired":
			probe.PairedAt = p.PairedAt.Add(-time.Minute)
		case "brief-unknown-owner":
			probe.Owner = "ou_other"
		case "brief-expired":
			probe.ExpiresAt = time.Now().Add(-time.Second)
		}
		if err := w.feishuErr(assistantwire.FeishuBriefPost, probe); err == nil {
			t.Errorf("unsafe send was accepted for %s", key)
		}
	}
}
