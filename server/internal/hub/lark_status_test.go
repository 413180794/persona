package hub

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// 设置 › 连接器 › 飞书 with no lark-connector in hub.json says so, without trying to reach one.
func TestLarkStatusUnconfigured(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Lark = lark.Config{}
	st := h.larkStatus(context.Background())
	if st.Configured || st.Connected || st.Error != "" {
		t.Fatalf("%+v", st)
	}
}
