package extmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

func (h *Manager) extensionChannelNotice(ctx context.Context, extensionID, id, method string, raw json.RawMessage) (any, error) {
	if !h.extensions.reg.HubScopeGranted(extensionID, extension.ScopeChannelNotice) {
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: "channel.notice grant required"}
	}
	bad := &rpc.Error{Code: "invalid_params", Message: "invalid passive notice parameters"}
	if len(raw) > 48*1024 {
		return nil, bad
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if method == assistantwire.MNoticeStatus {
		var p assistantwire.NoticeStatusParams
		if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Contribution != id {
			return nil, bad
		}
		var out assistantwire.NoticeStatusResult
		err := h.callAssistant(ctx, assistantwire.MethodChannelNoticeStatus, struct{}{}, &out)
		return out, err
	}
	var p assistantwire.NoticeParams
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Contribution != id || p.Notice.Validate() != nil {
		return nil, bad
	}
	var out assistantwire.NoticeResult
	err := h.callAssistant(ctx, assistantwire.MethodChannelNotice,
		assistantwire.ChannelNotice{Extension: extensionID, Channel: id, Notice: p.Notice}, &out)
	return out, err
}
