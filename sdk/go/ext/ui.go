package ext

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/sdk/go/wire"
)

// Page is one ui contribution (docs/extensions.md「声明式 UI」): what it draws, and what its buttons do.
type Page struct {
	// Render draws the page for r (its state, the slot, the app's context).
	Render func(ctx context.Context, r *UIRequest) (*wire.View, error)
	// Action answers a tap on something with an action (r.Action is its id, r.Values the page's fields when it
	// submits). Nil, or an empty result, draws the page again.
	Action func(ctx context.Context, r *UIRequest) (*wire.ActionResult, error)
}

// UIRequest is a ui.render or ui.action: wire.UIParams, and the hub to call while drawing (gateway.http …).
type UIRequest struct {
	wire.UIParams
	Hub *Hub
}

// DecodeState reads the page's state, which the program wrote as JSON (EncodeState); an empty state leaves v as is.
func (r *UIRequest) DecodeState(v any) error {
	if r.State == "" {
		return nil
	}
	return json.Unmarshal([]byte(r.State), v)
}

// EncodeState writes v as a page's state (at most 16 KB).
func EncodeState(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *server) ui(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	r := &UIRequest{Hub: s.hub}
	if err := json.Unmarshal(raw, &r.UIParams); err != nil {
		return nil, &Error{Code: "invalid", Message: err.Error()}
	}
	page, ok := s.prog.UI[r.Contribution]
	if !ok {
		return nil, NoContribution(r.Contribution)
	}
	switch method {
	case "render":
		if page.Render == nil {
			return nil, UnknownMethod("ui.render")
		}
		return page.Render(ctx, r)
	case "action":
		if page.Action != nil {
			res, err := page.Action(ctx, r)
			if err != nil || res != nil && (res.View != nil || res.Toast != "" || res.OpenURL != "") {
				return res, err
			}
		}
		if page.Render == nil {
			return wire.ActionResult{}, nil
		}
		v, err := page.Render(ctx, r)
		if err != nil {
			return nil, err
		}
		return wire.ActionResult{View: v}, nil
	}
	return nil, UnknownMethod("ui." + method)
}
