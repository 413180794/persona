// Package conformance holds a channel program to the channel extension point's protocol (docs/extensions/channels.md "Protocol"),
// after the plugin protocol's own checks (plugin/conformance): every channel the manifest contributes answers
// channel.send (sent, or cannot_send while there is nowhere to send to) the same way for the same ref, takes
// channel.typing, refuses a channel it does not have and a method it does not know; what both sides send holds to
// schema/extension/channel.schema.json.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/extension"
	pconf "github.com/nwverse/persona/server/internal/extension/conformance"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Check opens the program with initialize (init, its id and version from m) and holds each of m's channels to the
// protocol. Received are the channel.receive calls the program made meanwhile (a program may hand over nothing).
func Check(t testing.TB, m *extension.Manifest, run pconf.Run, init extension.InitializeParams) (received []channel.ReceiveParams) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hub := rpc.New("h", run.ToProgram)
	hub.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		// What the program sends is the schema's (schema/extension/channel.schema.json).
		if _, described := wire.Calls[method]; described {
			if err := wire.ValidateParams(method, params); err != nil {
				t.Errorf("%s: the params do not hold to schema/extension: %v", method, err)
			}
		}
		if method == channel.InPlugin(channel.MReceive) {
			var p channel.ReceiveParams
			if err := json.Unmarshal(params, &p); err != nil {
				t.Errorf("channel.receive: %v", err)
			}
			received = append(received, p)
		}
		return struct{}{}, nil
	}
	go func() { _ = hub.Serve(ctx, run.FromProgram) }()
	defer run.ToProgram.Close()
	init.Protocol = extension.Protocol
	init.Extension.ID, init.Extension.Version = m.ID, m.Version
	var res extension.InitializeResult
	if err := hub.Call(ctx, extension.MInitialize, init, &res); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	code := func(err error) string {
		var re *rpc.Error
		if errors.As(err, &re) {
			return re.Code
		}
		return ""
	}
	// holds holds a call's params and its answer (when it answered) to the schema.
	holds := func(method string, params any, answer json.RawMessage, err error) {
		t.Helper()
		raw, _ := json.Marshal(params)
		if serr := wire.ValidateParams(method, raw); serr != nil {
			t.Errorf("%s: the hub's params do not hold to schema/extension: %v", method, serr)
		}
		if serr := wire.ValidateResult(method, answer); err == nil && serr != nil {
			t.Errorf("%s: the answer does not hold to schema/extension: %v", method, serr)
		}
	}
	for _, c := range m.Channels() {
		send := channel.SendParams{Contribution: c.ID, Ref: "conformance-1", Text: "一致性测试", Kind: channel.OutNote}
		var first, second json.RawMessage
		err1 := hub.Call(ctx, channel.InPlugin(channel.MSend), send, &first)
		holds(channel.InPlugin(channel.MSend), send, first, err1)
		err2 := hub.Call(ctx, channel.InPlugin(channel.MSend), send, &second)
		switch {
		case err1 != nil && code(err1) != channel.CodeCannotSend:
			t.Errorf("%s: channel.send: %v (sent, or cannot_send)", c.ID, err1)
		case code(err1) != code(err2) || err1 == nil && string(first) != string(second):
			t.Errorf("%s: the same ref answered %s / %v, then %s / %v", c.ID, first, err1, second, err2)
		}
		typing := channel.TypingParams{Contribution: c.ID, On: false}
		var typed json.RawMessage
		if err := hub.Call(ctx, channel.InPlugin(channel.MTyping), typing, &typed); err != nil {
			t.Errorf("%s: channel.typing: %v", c.ID, err)
		} else {
			holds(channel.InPlugin(channel.MTyping), typing, typed, nil)
		}
		if err := hub.Call(ctx, channel.InPlugin(channel.MSend), channel.SendParams{Contribution: c.ID, Text: "x"}, nil); err == nil {
			t.Errorf("%s: channel.send without a ref was taken", c.ID)
		}
	}
	err := hub.Call(ctx, channel.InPlugin(channel.MSend), channel.SendParams{Contribution: "nochannel", Ref: "r", Text: "x"}, nil)
	if code(err) != extension.CodeNoContribution {
		t.Errorf("channel.send to a channel it does not have: %v, want %s", err, extension.CodeNoContribution)
	}
	if err := hub.Call(ctx, channel.InPlugin("nosuch"), map[string]any{}, nil); code(err) != extension.CodeUnknownMethod {
		t.Errorf("an unknown channel method: %v, want %s", err, extension.CodeUnknownMethod)
	}
	return received
}
