package extmgr

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
)

func TestPassiveChannelNoticeGrantAndAttribution(t *testing.T) {
	ctx := context.Background()
	method := assistantwire.MReceiveNotice
	input := json.RawMessage(`{"contribution":"examplechat","notice":{"id":"lc_one","kind":"grounded_reply","source_url":"https://applink.feishu.cn/client/chat/open?chatId=9&messageId=7","text":"passed","forward":"untrusted"}}`)
	for _, scopes := range [][]string{nil, {extension.ScopeNotify}} {
		w := newChannelWorld(t, scopes...)
		if _, err := w.h.extensionChannelCall(ctx, "example-chat", method, input); err == nil {
			t.Fatal("ungranted notice accepted")
		}
		if len(w.a.got(assistantwire.MethodChannelNotice)) != 0 {
			t.Fatal("ungranted notice crossed the bus")
		}
	}
	w := newChannelWorld(t, extension.ScopeChannelNotice)
	w.a.on(assistantwire.MethodChannelNotice, func(p json.RawMessage) (any, error) {
		var notice assistantwire.ChannelNotice
		if json.Unmarshal(p, &notice) != nil || notice.Extension != "example-chat" || notice.Channel != "examplechat" || notice.Forward != "untrusted" {
			t.Fatalf("invalid trusted attribution: %s", p)
		}
		return assistantwire.NoticeResult{ID: notice.ID, Status: "stored"}, nil
	})
	for _, bad := range []json.RawMessage{
		json.RawMessage(`{"contribution":"other","notice":{}}`),
		json.RawMessage(`{"contribution":"examplechat","extension":"owner","notice":{}}`),
		json.RawMessage(`{"contribution":"examplechat","notice":{"id":"lc_one","kind":"grounded_reply","source_url":"https://applink.feishu.cn.evil.test/client/chat/open?chatId=9&messageId=7","text":"passed","forward":"untrusted"}}`),
	} {
		if _, err := w.h.extensionChannelCall(ctx, "example-chat", method, bad); err == nil {
			t.Fatalf("accepted invalid payload: %s", bad)
		}
	}
	receipt, err := w.h.extensionChannelCall(ctx, "example-chat", method, input)
	if err != nil || receipt != (assistantwire.NoticeResult{ID: "lc_one", Status: "stored"}) {
		t.Fatalf("result: %+v, err: %v", receipt, err)
	}
	if len(w.a.got(assistantwire.MethodChannelNotice)) != 1 || len(w.received()) != 0 {
		t.Fatal("notice dispatched outside the display route")
	}
}
