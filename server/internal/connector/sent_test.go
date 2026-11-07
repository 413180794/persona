package connector

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// writer is fake with a second action, and no confirm_always.
type writer struct{ *fake }

func (w writer) Actions() []proto.Tool {
	return []proto.Tool{{Name: w.id + "_comment"}, {Name: w.id + "_note"}}
}

// broken is a connector whose writes fail.
type broken struct{ writer }

func (broken) Do(context.Context, string, json.RawMessage) (string, error) {
	return "", errors.New("the service said no")
}

type sentKey struct{}

// Only a write that went out through the gate is told to SetSent: with the rule that allowed it, or as the card the
// user allowed. A refusal, a decline, a failed Do and the user's own Run are not; neither is a read.
func TestSentOnlyWhatWentOut(t *testing.T) {
	ctx := context.WithValue(context.Background(), sentKey{}, "the call's")
	rules := []Rule{
		{Connector: "example", Action: "example_note", Decision: Deny},
		{Connector: "example", Action: "example_comment", Match: map[string]any{"repo": "allowed/*"}, Decision: Allow},
	}
	f := &fake{id: "example", status: Status{State: StateOK}}
	r := NewRegistry(rules)
	if err := r.Add(writer{f}); err != nil {
		t.Fatal(err)
	}
	var got []Sent
	var ctxs []any
	r.SetSent(func(ctx context.Context, s Sent) {
		got = append(got, s)
		ctxs = append(ctxs, ctx.Value(sentKey{}))
	})
	yes := func(context.Context, string, json.RawMessage) bool { return true }
	no := func(context.Context, string, json.RawMessage) bool { return false }
	left := func() []Sent { out := got; got = nil; return out }

	// A rule allowed it: told with the rule, what it was given and what it answered.
	args := json.RawMessage(`{"repo":"allowed/one","text":"hi"}`)
	if res := r.Call(ctx, "example_comment", args, no); res.IsError {
		t.Fatalf("rule: %+v", res)
	}
	s := left()
	if len(s) != 1 || s[0].Connector != "example" || s[0].Action != "example_comment" || string(s[0].Args) != string(args) || s[0].Result != "done" || s[0].Text != "hi" ||
		s[0].At.IsZero() || s[0].Through.Kind != ThroughRule || s[0].Through.Rule == nil || s[0].Through.Rule.Decision != Allow ||
		s[0].Through.Rule.Action != "example_comment" || s[0].Through.Rule.Match["repo"] != "allowed/*" || ctxs[0] != "the call's" {
		t.Fatalf("by rule: %+v (ctx %v)", s, ctxs)
	}

	// No rule matches and the user allows it on the phone: the card, with its words.
	other := json.RawMessage(`{"repo":"other/one","text":"hi"}`)
	if res := r.Call(ctx, "example_comment", other, yes); res.IsError {
		t.Fatalf("card: %+v", res)
	}
	s = left()
	if len(s) != 1 || s[0].Through.Kind != ThroughCard || s[0].Through.Rule != nil || s[0].Through.Summary != "评论 example-org/example-repo#12" {
		t.Fatalf("by card: %+v", s)
	}

	// Not told: declined, no way to ask, denied by a rule, the card could not be made, a read, the user's own Run.
	for name, call := range map[string]func() proto.ToolResult{
		"declined":   func() proto.ToolResult { return r.Call(ctx, "example_comment", other, no) },
		"no ask":     func() proto.ToolResult { return r.Call(ctx, "example_comment", other, nil) },
		"denied":     func() proto.ToolResult { return r.Call(ctx, "example_note", args, yes) },
		"bad args":   func() proto.ToolResult { return r.Call(ctx, "example_comment", json.RawMessage(`{"bad":true}`), yes) },
		"a read":     func() proto.ToolResult { return r.Call(ctx, "example_read", nil, yes) },
		"unknown":    func() proto.ToolResult { return r.Call(ctx, "example_other", nil, yes) },
		"own hand":   func() proto.ToolResult { return r.Run(ctx, "example_comment", other) },
		"own, noted": func() proto.ToolResult { return r.Run(ctx, "example_note", args) },
	} {
		call()
		if s := left(); len(s) != 0 {
			t.Errorf("%s was told: %+v", name, s)
		}
	}
	// The writes that ran: the rule's, the card's, and the user's two (the loop's order is not fixed).
	ran := slices.Sorted(slices.Values(f.did))
	if want := []string{"example_comment", "example_comment", "example_comment", "example_note"}; !slices.Equal(ran, want) {
		t.Fatalf("what ran: %v, want %v", ran, want)
	}

	// A Do that fails is not a write that went out, even through an allow rule.
	b := &fake{id: "broken", status: Status{State: StateOK}}
	r2 := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Allow}})
	_ = r2.Add(broken{writer{b}})
	r2.SetSent(func(context.Context, Sent) { t.Error("a failed write was told") })
	if res := r2.Call(ctx, "broken_comment", nil, yes); !res.IsError {
		t.Fatalf("the failure was lost: %+v", res)
	}
	// Nor one the user allowed on the phone that then failed.
	r3 := NewRegistry(nil)
	_ = r3.Add(broken{writer{&fake{id: "broken", status: Status{State: StateOK}}}})
	asked := false
	r3.SetSent(func(context.Context, Sent) { t.Error("a failed write allowed on a card was told") })
	if res := r3.Call(ctx, "broken_comment", json.RawMessage(`{"repo":"o/r","text":"hi"}`),
		func(context.Context, string, json.RawMessage) bool { asked = true; return true }); !res.IsError || !asked {
		t.Fatalf("card, then a failed write: %+v (asked %v)", res, asked)
	}

	// Without a SetSent, or with it cleared, writes run as before.
	r.SetSent(nil)
	if res := r.Call(ctx, "example_comment", args, no); res.IsError || len(left()) != 0 {
		t.Fatalf("without a listener: %+v", res)
	}
}

// What clamps an allow to a card (confirm_always, ConfirmEach) is told as the card, not as the rule that said allow.
func TestSentClampedAllowIsACard(t *testing.T) {
	ctx := context.Background()
	f := &fake{id: "example", status: Status{State: StateOK}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Allow}})
	_ = r.Add(guarded{f}) // example_comment is confirm_always, example_note is not
	var got []Sent
	r.SetSent(func(_ context.Context, s Sent) { got = append(got, s) })
	yes := func(context.Context, string, json.RawMessage) bool { return true }

	r.Call(ctx, "example_comment", nil, yes)
	r.Call(ctx, "example_note", nil, yes)
	r.Call(ConfirmEach(ctx), "example_note", nil, yes)
	if len(got) != 3 || got[0].Through.Kind != ThroughCard || got[1].Through.Kind != ThroughRule || got[2].Through.Kind != ThroughCard ||
		got[0].Through.Rule != nil || got[2].Through.Rule != nil {
		t.Fatalf("told %+v", got)
	}
}

// The text a write puts out is the arguments' own words, whatever the connector calls them, cut to 4000 characters.
func TestSentText(t *testing.T) {
	long := strings.Repeat("字", 4010) // the literal, not SentTextMost: the limit itself is what is tested
	for _, c := range []struct{ args, want string }{
		{`{"repo":"o/r","number":1,"body":"a comment"}`, "a comment"},
		{`{"chat":"c","text":"hello","reply_to":3}`, "hello"},
		{`{"body":"","text":"second choice"}`, "second choice"},
		{`{"subject":"s"}`, ""},
		{`{"text":42}`, ""},
		{`not json`, ""},
		{`{"text":"` + long + `"}`, strings.Repeat("字", 4000)},
	} {
		if got := writtenText(json.RawMessage(c.args)); got != c.want {
			t.Errorf("%.40s: %.20q (%d characters), want %.20q", c.args, got, len([]rune(got)), c.want)
		}
	}
}

// refWriter is a writer that also says which message it wrote (RefActions); its Do is the text alone, as a connector
// without the interface would answer.
type refWriter struct {
	writer
	ref string
	err error
}

func (w refWriter) DoRef(ctx context.Context, name string, args json.RawMessage) (DoResult, error) {
	text, _ := w.writer.Do(ctx, name, args)
	return DoResult{Text: text, MessageRef: w.ref}, w.err
}

// A connector that says which message it wrote has that id told with the write, and the model still gets the same
// text. One that does not (its Do alone) is told as before, with no id; a write that failed, or one the user sent by
// their own hand, is not told at all.
func TestSentCarriesTheMessageRef(t *testing.T) {
	ctx := context.Background()
	allow := []Rule{{Connector: "*", Action: "*", Decision: Allow}}
	told := func(c Connector) (proto.ToolResult, []Sent) {
		r := NewRegistry(allow)
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
		var got []Sent
		r.SetSent(func(_ context.Context, s Sent) { got = append(got, s) })
		return r.Call(ctx, c.ID()+"_comment", json.RawMessage(`{"text":"hi"}`), nil), got
	}

	res, got := told(refWriter{writer: writer{&fake{id: "example", status: Status{State: StateOK}}}, ref: "c-17"})
	if res.IsError || res.Content[0].Text != "done" || len(got) != 1 || got[0].MessageRef != "c-17" || got[0].Result != "done" {
		t.Fatalf("a connector with RefActions: %+v, told %+v", res, got)
	}
	if res, got = told(writer{&fake{id: "example", status: Status{State: StateOK}}}); res.IsError || len(got) != 1 || got[0].MessageRef != "" {
		t.Fatalf("a connector without it: %+v, told %+v", res, got)
	}
	if res, got = told(refWriter{writer: writer{&fake{id: "example", status: Status{State: StateOK}}}, ref: "c-18", err: errors.New("no")}); !res.IsError || len(got) != 0 {
		t.Fatalf("a failed write: %+v, told %+v", res, got)
	}

	r := NewRegistry(allow)
	_ = r.Add(refWriter{writer: writer{&fake{id: "example", status: Status{State: StateOK}}}, ref: "c-19"})
	r.SetSent(func(context.Context, Sent) { t.Error("the user's own write was told") })
	if res := r.Run(ctx, "example_comment", json.RawMessage(`{"text":"hi"}`)); res.IsError || res.Content[0].Text != "done" {
		t.Fatalf("the user's own write: %+v", res)
	}
}
