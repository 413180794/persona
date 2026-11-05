package connector

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// failing is fake whose read and write fail with err.
type failing struct {
	*fake
	err error
}

func (f failing) Call(context.Context, string, json.RawMessage) (string, error) { return "", f.err }
func (f failing) Do(context.Context, string, json.RawMessage) (string, error)   { return "", f.err }

// The registry names the kind of each failure it knows (#3779), so personad tool does not guess it from the words: a
// decline by the user or by connector_gate, a connector not signed in or rate limited, a program's rate_limited
// error and its retry_after. Other errors stay plain failures.
func TestFailuresSayTheirKind(t *testing.T) {
	ctx := context.Background()
	no := func(context.Context, string, json.RawMessage) bool { return false }
	yes := func(context.Context, string, json.RawMessage) bool { return true }
	add := func(rules []Rule, c Connector) *Registry {
		r := NewRegistry(rules)
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
		return r
	}
	ok := Status{State: StateOK}
	limited := &rpc.Error{Code: "rate_limited", Message: "Example 限流，30 秒后再试", Data: json.RawMessage(`{"retry_after":29.2}`)}

	cases := []struct {
		name  string
		res   proto.ToolResult
		kind  string
		retry int
	}{
		{"declined on the phone", add(nil, &fake{id: "example", status: ok}).Call(ctx, "example_comment", nil, no), proto.ToolErrorDeclined, 0},
		{"denied by the rules", add([]Rule{{Connector: "*", Action: "*", Decision: Deny}}, &fake{id: "example", status: ok}).Call(ctx, "example_comment", nil, yes), proto.ToolErrorDeclined, 0},
		{"not signed in", add(nil, &fake{id: "example", status: Status{State: StateNotSignedIn}}).Call(ctx, "example_read", nil, nil), proto.ToolErrorUnavailable, 0},
		{"status rate limited", add(nil, &fake{id: "example", status: Status{State: StateRateLimited, Error: "限流"}}).Call(ctx, "example_read", nil, nil), proto.ToolErrorRateLimited, 0},
		{"read rate limited", add(nil, failing{&fake{id: "example", status: ok}, limited}).Call(ctx, "example_read", nil, nil), proto.ToolErrorRateLimited, 30},
		{"write rate limited", add(nil, failing{&fake{id: "example", status: ok}, limited}).Call(ctx, "example_comment", nil, yes), proto.ToolErrorRateLimited, 30},
		{"wrapped", add(nil, failing{&fake{id: "example", status: ok}, errors.Join(errors.New("call"), limited)}).Call(ctx, "example_read", nil, nil), proto.ToolErrorRateLimited, 30},
		{"other code", add(nil, failing{&fake{id: "example", status: ok}, &rpc.Error{Code: "invalid", Message: "bad chat"}}).Call(ctx, "example_read", nil, nil), "", 0},
		{"plain error", add(nil, failing{&fake{id: "example", status: ok}, errors.New("chat not found")}).Call(ctx, "example_read", nil, nil), "", 0},
	}
	for _, c := range cases {
		if !c.res.IsError || c.res.ErrorKind != c.kind || c.res.RetryAfter != c.retry {
			t.Errorf("%s: %+v, want kind %q retry %d", c.name, c.res, c.kind, c.retry)
		}
	}
}
