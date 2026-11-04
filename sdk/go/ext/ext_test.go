package ext

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

// wireClient is the hub's end of the pipes: one request at a time, answers in order.
type wireClient struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Scanner
	n   int
}

func start(t *testing.T, prog Program) *wireClient {
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	go func() { _ = Serve(context.Background(), progR, progW, prog); _ = progW.Close() }()
	t.Cleanup(func() { hubW.Close() })
	return &wireClient{t: t, in: hubW, out: bufio.NewScanner(hubR)}
}

func (c *wireClient) call(method string, params any) frame {
	c.t.Helper()
	c.n++
	raw, _ := json.Marshal(params)
	b, _ := json.Marshal(frame{ID: "h" + string(rune('0'+c.n)), Method: method, Params: raw})
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
	for c.out.Scan() {
		var f frame
		if json.Unmarshal(c.out.Bytes(), &f) == nil && f.ID != "" && f.Method == "" {
			return f
		}
	}
	c.t.Fatalf("%s: no answer", method)
	return frame{}
}

type coded struct{}

func (coded) Error() string                        { return "no such thing" }
func (coded) WireError() (string, json.RawMessage) { return "not_found", json.RawMessage(`{"id":"x"}`) }

func TestServe(t *testing.T) {
	var config struct {
		Name string `json:"name"`
	}
	c := start(t, Program{ID: "example", Version: "1.0.0",
		Init: func(ctx context.Context, in *Init, hub *Hub) (map[string]any, error) {
			return nil, in.Config(&config)
		},
		Namespaces: map[string]Handler{"thing": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			return nil, coded{}
		}},
		UI: map[string]Page{"home": {
			Render: func(ctx context.Context, r *UIRequest) (*wire.View, error) {
				return &wire.View{Title: r.Action + "|" + r.State, Sections: []wire.Section{}}, nil
			},
			Action: func(ctx context.Context, r *UIRequest) (*wire.ActionResult, error) {
				if r.Action == "toast" {
					return &wire.ActionResult{Toast: "好"}, nil
				}
				r.State = "next"
				return nil, nil
			},
		}},
	})
	if f := c.call("ping", nil); f.Error == nil || f.Error.Code != wire.ErrorCodeNotInitialized {
		t.Fatalf("ping before initialize: %+v", f)
	}
	// A hub from before the rename sends the extension under plugin.
	f := c.call("initialize", map[string]any{"protocol": 1, "hub": map[string]any{"build": 1}, "plugin": map[string]any{"id": "example", "version": "1.0.0"},
		"config": map[string]any{"name": "示例"}, "granted": map[string]any{}})
	if f.Error != nil || config.Name != "示例" {
		t.Fatalf("initialize: %+v %+v", f, config)
	}
	if f := c.call("thing.get", nil); f.Error == nil || f.Error.Code != "not_found" || string(f.Error.Data) != `{"id":"x"}` {
		t.Fatalf("a coded error: %+v", f.Error)
	}
	if f := c.call("ui.render", wire.UIParams{Contribution: "nope"}); f.Error == nil || f.Error.Code != wire.ErrorCodeNoContribution {
		t.Fatalf("ui.render of another contribution: %+v", f.Error)
	}
	var res wire.ActionResult
	f = c.call("ui.action", wire.UIParams{Contribution: "home", Action: "more", State: "first"})
	if f.Error != nil || json.Unmarshal(f.Result, &res) != nil || res.View == nil || res.View.Title != "more|next" {
		t.Fatalf("an action that answers nothing draws again, with the state it set: %s %+v", f.Result, f.Error)
	}
	res = wire.ActionResult{}
	f = c.call("ui.action", wire.UIParams{Contribution: "home", Action: "toast"})
	if json.Unmarshal(f.Result, &res) != nil || res.Toast != "好" || res.View != nil {
		t.Fatalf("toast: %s", f.Result)
	}
	if f := c.call("other.method", nil); f.Error == nil || f.Error.Code != wire.ErrorCodeUnknownMethod {
		t.Fatalf("unknown method: %+v", f.Error)
	}
}
