package connector

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// fake is a connector with every capability, on made-up data.
type fake struct {
	id     string
	status Status
	did    []string
	args   []string // what each Do was given
	watch  func(ctx context.Context, changed func(Scope)) error
}

func (f *fake) Manifest() Manifest {
	return Manifest{Capabilities: []Capability{CapStatus, CapTools, CapActions, CapEvents}, Hosts: []string{"example.com"}}
}

func (f *fake) ID() string                    { return f.id }
func (f *fake) Name() string                  { return "Example" }
func (f *fake) Status(context.Context) Status { return f.status }
func (f *fake) Tools() []proto.Tool           { return []proto.Tool{{Name: f.id + "_read"}, {Name: "x_read"}} }
func (f *fake) Actions() []proto.Tool         { return []proto.Tool{{Name: f.id + "_comment"}} }

func (f *fake) Watch(ctx context.Context, changed func(Scope)) error { return f.watch(ctx, changed) }

func (f *fake) Call(_ context.Context, name string, args json.RawMessage) (string, error) {
	return "read " + string(args), nil
}

func (f *fake) Describe(_ context.Context, name string, args json.RawMessage) (Action, string, error) {
	if string(args) == `{"bad":true}` {
		return Action{}, "", errors.New("bad arguments")
	}
	return Action{Name: name, Visible: "example-org/example-repo#12 的所有参与者"}, "评论 example-org/example-repo#12", nil
}

func (f *fake) Do(_ context.Context, name string, args json.RawMessage) (string, error) {
	f.did = append(f.did, name)
	f.args = append(f.args, string(args))
	return "done", nil
}

// unlistedFake is fake with its action unlisted.
type unlistedFake struct{ *fake }

func (f unlistedFake) Manifest() Manifest {
	m := f.fake.Manifest()
	m.Unlisted = []string{"*_comment"}
	return m
}

// An unlisted tool is not in the models' lists, yet is called by name like any other: an action still goes through
// the gate and runs with the very arguments the card was made from.
func TestUnlistedTools(t *testing.T) {
	ctx := context.Background()
	f := &fake{id: "example", status: Status{State: StateOK}}
	r := NewRegistry(nil)
	if err := r.Add(unlistedFake{f}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range r.Tools(ctx) {
		names = append(names, tl.Name)
	}
	if !slices.Equal(names, []string{"example_read"}) {
		t.Fatalf("listed %v", names)
	}
	if !r.Offers("example_comment") {
		t.Fatal("an unlisted action cannot be called by name")
	}
	if res := r.Call(ctx, "example_comment", json.RawMessage(`{"text":"示例"}`), nil); !res.IsError || len(f.did) != 0 {
		t.Fatalf("an unlisted action ran without the gate: %+v", res)
	}
	var cardArgs string
	yes := func(_ context.Context, _ string, in json.RawMessage) bool { cardArgs = string(in); return true }
	if res := r.Call(ctx, "example_comment", json.RawMessage(`{"text":"示例"}`), yes); res.IsError || len(f.did) != 1 {
		t.Fatalf("allowed but did not run: %+v", res)
	}
	if cardArgs != `{"text":"示例"}` || f.args[0] != cardArgs {
		t.Fatalf("the card was made from %s, it ran with %s", cardArgs, f.args[0])
	}
}

// A card shows at most cardMost of Describe's words and says how many it left out.
func TestCardIsCut(t *testing.T) {
	if s := card("评论 example-org/example-repo#12"); s != "评论 example-org/example-repo#12" {
		t.Fatalf("a short one changed: %q", s)
	}
	s := card(strings.Repeat("字", cardMost+5))
	if len([]rune(s)) > cardMost+40 || !strings.HasSuffix(s, "（后面还有 5 字，卡片只显示前 16000 字）") {
		t.Fatalf("cut to %d runes, ending %q", len([]rune(s)), string([]rune(s)[len([]rune(s))-30:]))
	}
}

// plain has no capability at all.
type plain struct{}

func (plain) ID() string         { return "plain" }
func (plain) Name() string       { return "Plain" }
func (plain) Manifest() Manifest { return Manifest{} }

func TestRegistryRejectsBadAndDuplicateIDs(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.Add(&fake{id: "example"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"example", "Example", "ex_ample", "", "1x"} {
		if err := r.Add(&fake{id: id}); err == nil {
			t.Errorf("%q was accepted", id)
		}
	}
	if len(r.All()) != 1 || r.Get("example") == nil || r.Get("nope") != nil {
		t.Fatalf("registry: %v", r.All())
	}
	var none *Registry
	if none.All() != nil || none.Get("example") != nil || none.Tools(context.Background()) != nil || len(none.Infos(context.Background())) != 0 {
		t.Fatal("a nil registry is not empty")
	}
}

func TestToolsOnlyFromOKConnectorsAndOnlyTheirOwn(t *testing.T) {
	ok := &fake{id: "example", status: Status{State: StateOK}}
	off := &fake{id: "signedout", status: Status{State: StateNotSignedIn, Error: "未登录"}}
	r := NewRegistry(nil)
	for _, c := range []Connector{ok, off, plain{}} {
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, tl := range r.Tools(context.Background()) {
		names = append(names, tl.Name)
	}
	if want := []string{"example_read", "example_comment"}; !slices.Equal(names, want) {
		t.Fatalf("tools %v, want %v", names, want)
	}
	if res := r.Call(context.Background(), "signedout_read", nil, nil); !res.IsError || !contains(res, "未登录") {
		t.Fatalf("a signed-out connector's tool: %+v", res)
	}
	if res := r.Call(context.Background(), "x_read", nil, nil); !res.IsError {
		t.Fatalf("someone else's tool ran: %+v", res)
	}
	if res := r.Call(context.Background(), "example_read", json.RawMessage(`{"n":1}`), nil); res.IsError || !contains(res, `read {"n":1}`) {
		t.Fatalf("read: %+v", res)
	}
}

func TestActionsGoThroughTheGate(t *testing.T) {
	ctx := context.Background()
	asked := ""
	yes := func(_ context.Context, summary string, _ json.RawMessage) bool { asked = summary; return true }
	no := func(_ context.Context, summary string, _ json.RawMessage) bool { asked = summary; return false }

	// No rule: the user confirms; declining does nothing; no way to ask is a no.
	c := &fake{id: "example", status: Status{State: StateOK}}
	r := NewRegistry(nil)
	_ = r.Add(c)
	if res := r.Call(ctx, "example_comment", nil, no); !res.IsError || len(c.did) != 0 {
		t.Fatalf("declined but ran: %+v %v", res, c.did)
	}
	if !strings.Contains(asked, "谁会看到：example-org/example-repo#12 的所有参与者") {
		t.Fatalf("the card does not say who sees it: %q", asked)
	}
	if res := r.Call(ctx, "example_comment", nil, nil); !res.IsError || len(c.did) != 0 {
		t.Fatalf("ran without asking: %+v", res)
	}
	if res := r.Call(ctx, "example_comment", nil, yes); res.IsError || len(c.did) != 1 {
		t.Fatalf("allowed but did not run: %+v", res)
	}
	asked = ""
	if res := r.Call(ctx, "example_comment", json.RawMessage(`{"bad":true}`), yes); !res.IsError || !contains(res, "bad arguments") || asked != "" || len(c.did) != 1 {
		t.Fatalf("arguments Describe refuses were asked about or ran: %+v, asked %q", res, asked)
	}

	// Rules: the first match decides, fields narrow a rule.
	rules := []Rule{
		{Connector: "example", Action: "example_comment", Match: map[string]any{"repo": "example-org/*"}, Decision: Allow},
		{Connector: "*", Action: "*_comment", Decision: Deny},
	}
	if err := ValidateRules(rules); err != nil {
		t.Fatal(err)
	}
	c = &fake{id: "example", status: Status{State: StateOK}}
	r = NewRegistry(rules)
	_ = r.Add(c)
	asked = ""
	if res := r.Call(ctx, "example_comment", json.RawMessage(`{"repo":"example-org/example-repo"}`), no); res.IsError || asked != "" {
		t.Fatalf("allow rule: %+v, asked %q", res, asked)
	}
	if res := r.Call(ctx, "example_comment", json.RawMessage(`{"repo":"elsewhere/x"}`), yes); !res.IsError || asked != "" || len(c.did) != 1 {
		t.Fatalf("deny rule: %+v, asked %q, did %v", res, asked, c.did)
	}
}

func TestValidateRules(t *testing.T) {
	for _, r := range []Rule{
		{Connector: "*", Action: "*", Decision: "yes"},
		{Connector: "", Action: "*", Decision: Allow},
		{Connector: "[", Action: "*", Decision: Allow},
		{Connector: "lark", Action: "lark_send", Decision: Allow},
		{Connector: "lark", Action: "POST conversations", Decision: Allow},
	} {
		if ValidateRules([]Rule{r}) == nil {
			t.Errorf("%+v was accepted", r)
		}
	}
}

// connector_gate's rules of connector "lark" become the Feishu gate's rules (node/larkgate.go), ahead of lark_gate's.
func TestLarkRules(t *testing.T) {
	rules := []Rule{
		{Connector: "github", Action: "github_comment", Decision: Allow},
		{Connector: "lark", Action: "post /conversations/*/messages", Match: map[string]any{"msg_type": "text"}, Decision: Allow},
		{Connector: "lark", Action: "CLI /docs/**", Decision: Deny},
		{Connector: "lark", Action: "*", Decision: Confirm},
	}
	if err := ValidateRules(rules); err != nil {
		t.Fatal(err)
	}
	old := []proto.LarkRule{{Method: "DELETE", Path: "/messages/*", Action: "deny"}}
	want := []proto.LarkRule{
		{Method: "POST", Path: "/conversations/*/messages", Body: map[string]any{"msg_type": "text"}, Action: "allow"},
		{Method: "CLI", Path: "/docs/**", Action: "deny"},
		{Method: "*", Path: "**", Action: "confirm"},
		old[0],
	}
	if got := LarkRules(rules, old); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if got := LarkRules(nil, old); !reflect.DeepEqual(got, old) {
		t.Fatalf("lark_gate alone: %+v", got)
	}
}

func TestInfos(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	r := NewRegistry(nil)
	_ = r.Add(&fake{id: "example", status: Status{State: StateOK, Account: "alice", LastSync: at}})
	_ = r.Add(plain{})
	_ = r.Add(&fake{id: "broken", status: Status{State: StateNoPermission, Error: "不让读", Guide: []string{"打开设置", "打开开关"}}})
	b, _ := json.Marshal(r.Infos(context.Background()))
	want := `[{"id":"example","name":"Example","status":"ok","account":"alice","last_sync":"2026-01-02T03:04:05Z",` +
		`"capabilities":["status","tools","actions","events","hosts"],"hosts":["example.com"],"enabled":true},` +
		`{"id":"plain","name":"Plain","status":"ok","capabilities":[],"enabled":true},` +
		`{"id":"broken","name":"Example","status":"no_permission","error":"不让读","guide":["打开设置","打开开关"],` +
		`"capabilities":["status","tools","actions","events","hosts"],"hosts":["example.com"],"enabled":true}]`
	if string(b) != want {
		t.Fatalf("infos\n%s\nwant\n%s", b, want)
	}
}

func TestWatchRestartsAfterAFailure(t *testing.T) {
	watchBackoffFirst = time.Millisecond
	t.Cleanup(func() { watchBackoffFirst = 5 * time.Second })
	var runs atomic.Int32
	c := &fake{id: "example", watch: func(ctx context.Context, changed func(Scope)) error {
		changed(ScopeInbox)
		if runs.Add(1) < 3 {
			return errors.New("socket closed")
		}
		<-ctx.Done()
		return nil
	}}
	r := NewRegistry(nil)
	_ = r.Add(c)
	_ = r.Add(plain{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	kicks := make(chan string, 10)
	r.Start(ctx, func(id string, scope Scope) { kicks <- id + " " + string(scope) }, func(string, ...any) {})
	for i := 0; i < 3; i++ {
		select {
		case k := <-kicks:
			if k != "example inbox" {
				t.Fatalf("kick %q", k)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d watches ran", i)
		}
	}
}

func contains(r proto.ToolResult, s string) bool {
	return len(r.Content) == 1 && strings.Contains(r.Content[0].Text, s)
}

// Run is the user's own write (the app's reply box): it runs without asking, whatever the rules say, but not on a
// connector that is not available or for a tool the connector does not have.
func TestRunSkipsTheGate(t *testing.T) {
	ctx := context.Background()
	c := &fake{id: "example", status: Status{State: StateOK}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Deny}})
	_ = r.Add(c)
	if res := r.Run(ctx, "example_comment", nil); res.IsError || len(c.did) != 1 {
		t.Fatalf("the user's own write did not run: %+v %v", res, c.did)
	}
	if res := r.Call(ctx, "example_comment", nil, nil); !res.IsError || len(c.did) != 1 {
		t.Fatalf("the AI's got past a deny rule: %+v %v", res, c.did)
	}
	if res := r.Run(ctx, "example_nope", nil); !res.IsError || !contains(res, "unknown tool") {
		t.Fatalf("unknown tool: %+v", res)
	}
	c.status = Status{State: StateNotSignedIn}
	if res := r.Run(ctx, "example_comment", nil); !res.IsError || len(c.did) != 1 {
		t.Fatalf("ran while signed out: %+v %v", res, c.did)
	}
}
