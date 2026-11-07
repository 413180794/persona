package connector

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// guarded is fake with a second action, and its comments listed in confirm_always.
type guarded struct{ *fake }

func (g guarded) Manifest() Manifest {
	m := g.fake.Manifest()
	m.ConfirmAlways = []string{"*_comment"}
	return m
}

func (g guarded) Actions() []proto.Tool {
	return []proto.Tool{{Name: g.id + "_comment"}, {Name: g.id + "_note"}}
}

// confirm_always clamps an allow to confirm and leaves deny, the unlisted actions and the connectors without it as
// they were.
func TestConfirmAlwaysClampsAllow(t *testing.T) {
	m := Manifest{ConfirmAlways: []string{"example_comment"}}
	allowAll := []Rule{{Connector: "*", Action: "*", Decision: Allow}}
	denyAll := []Rule{{Connector: "*", Action: "*", Decision: Deny}}
	for _, c := range []struct {
		name  string
		rules []Rule
		m     Manifest
		tool  string
		want  string
	}{
		{"allow is clamped", allowAll, m, "example_comment", Confirm},
		{"deny stays", denyAll, m, "example_comment", Deny},
		{"no rule is confirm", nil, m, "example_comment", Confirm},
		{"other actions are still allowed", allowAll, m, "example_note", Allow},
		{"not declared: unchanged", allowAll, Manifest{}, "example_comment", Allow},
	} {
		if got := Decide(c.rules, c.m, "example", c.tool, nil); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// A call that gives a field more than once is refused whatever the rules say, and no rule is read for it: a connector
// that decodes into a struct reads such a call by rules the gate's map does not follow, so the gate cannot know which
// value the write would use. Two names are the same field when they are written alike or differ only in capitals (by
// encoding/json's case folding). Only the top level counts, and a call that gives each field once is decided as ever.
func TestACallThatGivesAFieldMoreThanOnceIsRefused(t *testing.T) {
	allowRepo := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"repo": "example-org/*"}, Decision: Allow}
	allowAny := Rule{Connector: "github", Action: "github_comment", Decision: Allow}
	denyOther := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"repo": "other-org/other"}, Decision: Deny}
	confirmAny := Rule{Connector: "github", Action: "github_comment", Decision: Confirm}
	for _, c := range []struct {
		name  string
		rules []Rule
		args  string
		want  string
	}{
		{"a name in two capitals, an allow with a match", []Rule{allowRepo}, `{"repo":"example-org/a","REPO":"other-org/a"}`, Deny},
		{"the same, in the other order", []Rule{allowRepo}, `{"Repo":"other-org/a","repo":"example-org/a"}`, Deny},
		{"three spellings of a name", []Rule{allowAny}, `{"repo":"a","Repo":"b","REPO":"c"}`, Deny},
		{"two of three spellings alike", []Rule{allowAny}, `{"repo":"a","Repo":"b","repo":"c"}`, Deny},
		{"the very same name twice", []Rule{allowRepo}, `{"repo":"other-org/a","repo":"example-org/a"}`, Deny},
		{"the same name, once escaped", []Rule{allowRepo}, `{"re\u0070o":"other-org/a","repo":"example-org/a"}`, Deny},
		{"the second value is null", []Rule{allowRepo}, `{"repo":"example-org/a","repo":null}`, Deny},
		{"the second value is null, in other capitals", []Rule{allowRepo}, `{"repo":"example-org/a","REPO":null}`, Deny},
		{"the second value is an array", []Rule{allowAny}, `{"to":["a"],"to":["b"]}`, Deny},
		{"the second value is an object", []Rule{allowAny}, `{"target":{"a":1},"target":{"b":2}}`, Deny},
		{"another field twice, the one a rule reads once", []Rule{allowRepo}, `{"repo":"example-org/a","Body":"x","body":"y"}`, Deny},
		{"after a string value", []Rule{allowAny}, `{"a":"x","repo":"y","REPO":"z"}`, Deny},
		{"after a number", []Rule{allowAny}, `{"number":1,"repo":"y","REPO":"z"}`, Deny},
		{"after a boolean", []Rule{allowAny}, `{"draft":true,"repo":"y","REPO":"z"}`, Deny},
		{"after a null", []Rule{allowAny}, `{"x":null,"repo":"y","REPO":"z"}`, Deny},
		{"after an array", []Rule{allowAny}, `{"to":[1,{"a":2}],"repo":"y","REPO":"z"}`, Deny},
		{"after an object", []Rule{allowAny}, `{"target":{"a":{"b":[]}},"repo":"y","REPO":"z"}`, Deny},
		{"the empty name twice", []Rule{allowAny}, `{"":1,"":2}`, Deny},
		{"the Kelvin sign is a K", []Rule{allowAny}, `{"key":"x","\u212aey":"y"}`, Deny},
		{"the long s is an s", []Rule{allowAny}, `{"set":"x","\u017fet":"y"}`, Deny},
		{"a deny with a match that neither spelling meets", []Rule{denyOther, allowAny}, `{"repo":"example-org/a","REPO":"example-org/b"}`, Deny},
		{"a confirm", []Rule{confirmAny}, `{"repo":"example-org/a","REPO":"other-org/a"}`, Deny},
		{"no rule", nil, `{"repo":"example-org/a","REPO":"other-org/a"}`, Deny},
		// a call that gives each field once is decided as it always was
		{"plain, an allow with a match", []Rule{allowRepo}, `{"repo":"example-org/a","body":"x"}`, Allow},
		{"plain, no rule", nil, `{"repo":"example-org/a"}`, Confirm},
		{"the empty name once", []Rule{allowAny}, `{"":1,"a":2}`, Allow},
		{"a name that only starts like another", []Rule{allowRepo}, `{"repo":"example-org/a","repos":"x"}`, Allow},
		{"a letter of another script that only looks like it is another name", []Rule{allowRepo}, `{"repo":"example-org/a","\u0440epo":"other-org/a"}`, Allow},
		{"dotless i is not an i", []Rule{allowAny}, `{"id":"x","\u0131d":"y"}`, Allow},
		{"dotted capital I is not an i", []Rule{allowAny}, `{"id":"x","\u0130d":"y"}`, Allow},
		{"a name given twice inside a value is not the top level", []Rule{allowRepo}, `{"repo":"example-org/a","extra":{"x":1,"X":2,"x":3}}`, Allow},
		{"arguments that are not an object", []Rule{allowRepo}, `[1]`, Confirm},
		{"no arguments", []Rule{allowAny}, ``, Allow},
	} {
		if got := Decide(c.rules, Manifest{}, "github", "github_comment", json.RawMessage(c.args)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// What the model reads when a call is refused for giving a field more than once is enough to put it right, and nothing
// runs: not the write, not the card, whatever the rules would have said.
func TestARefusedCallSaysWhichFieldAndRunsNothing(t *testing.T) {
	ctx := context.Background()
	asked := 0
	ask := func(context.Context, string, json.RawMessage) bool { asked++; return true }
	c := guarded{&fake{id: "example", status: Status{State: StateOK}}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Allow}})
	_ = r.Add(c)

	for _, tc := range []struct{ name, args, field string }{
		{"in other capitals", `{"text":"a","TEXT":"b"}`, `"text"`},
		{"the very same name", `{"to":"a","to":"b"}`, `"to"`},
		{"the first spelling is the one named", `{"Reply":"a","reply":"b"}`, `"Reply"`},
	} {
		res := r.Call(ctx, "example_note", json.RawMessage(tc.args), ask)
		if !res.IsError || res.ErrorKind != proto.ToolErrorDeclined || !contains(res, tc.field) || !contains(res, "more than once") || !contains(res, "example_note") {
			t.Errorf("%s: %+v", tc.name, res)
		}
	}
	if asked != 0 || len(c.did) != 0 {
		t.Fatalf("a refused call went on: asked %d times, did %v", asked, c.did)
	}
	if res := r.Call(ctx, "example_note", json.RawMessage(`{"text":"a","to":"b"}`), ask); res.IsError || len(c.did) != 1 {
		t.Fatalf("a call that gives each field once did not run: %+v %v", res, c.did)
	}
	if res := r.Run(ctx, "example_note", json.RawMessage(`{"text":"a","TEXT":"b"}`)); res.IsError || len(c.did) != 2 {
		t.Fatalf("the user's own write is not gated: %+v %v", res, c.did)
	}
}

// A Deny or a Confirm with a Match reads a name whatever its capitals, so spelling a field in other capitals does not
// get a call past it, even when the call gives the field once; an Allow still reads the exact names, and the first rule
// that matches decides, as ever.
func TestADenyOrConfirmReadsANameWhateverItsCapitals(t *testing.T) {
	other := map[string]any{"repo": "other-org/other"}
	denyOther := Rule{Connector: "github", Action: "github_comment", Match: other, Decision: Deny}
	confirmOther := Rule{Connector: "github", Action: "github_comment", Match: other, Decision: Confirm}
	allowRepo := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"repo": "example-org/*"}, Decision: Allow}
	allowAny := Rule{Connector: "github", Action: "github_comment", Decision: Allow}
	denyAll := Rule{Connector: "github", Action: "*", Decision: Deny}
	denyPattern := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"repo": "other-org/*"}, Decision: Deny}
	denyTwoFields := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"repo": "other-org/other", "number": float64(12)}, Decision: Deny}
	denyKey := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"key": "evil"}, Decision: Deny}
	for _, c := range []struct {
		name  string
		rules []Rule
		args  string
		want  string
	}{
		{"a deny, the name in other capitals", []Rule{denyOther, allowAny}, `{"REPO":"other-org/other"}`, Deny},
		{"a deny, the name in other capitals, beside others", []Rule{denyOther, allowAny}, `{"Repo":"other-org/other","body":"x"}`, Deny},
		{"a deny with a pattern", []Rule{denyPattern, allowAny}, `{"REPO":"other-org/x"}`, Deny},
		{"a deny of two fields, one in other capitals", []Rule{denyTwoFields, allowAny}, `{"repo":"other-org/other","Number":12}`, Deny},
		{"a deny of two fields, the value of one is not met", []Rule{denyTwoFields, allowAny}, `{"repo":"other-org/other","Number":13}`, Allow},
		{"a deny, a letter that folds to the name", []Rule{denyKey, allowAny}, `{"\u212aey":"evil"}`, Deny},
		{"a deny, the value is not met", []Rule{denyOther, allowAny}, `{"REPO":"example-org/a"}`, Allow},
		{"a deny, the exact name, as before", []Rule{denyOther, allowAny}, `{"repo":"other-org/other"}`, Deny},
		{"a deny, no such field, as before", []Rule{denyOther, allowAny}, `{"body":"x"}`, Allow},
		{"a confirm, the name in other capitals", []Rule{confirmOther, denyAll}, `{"REPO":"other-org/other"}`, Confirm},
		{"a confirm, the exact name, as before", []Rule{confirmOther, denyAll}, `{"repo":"other-org/other"}`, Confirm},
		{"a confirm that the value does not meet passes the call on", []Rule{confirmOther, denyAll}, `{"REPO":"example-org/a"}`, Deny},
		{"an allow, the exact name, as before", []Rule{allowRepo}, `{"repo":"example-org/a"}`, Allow},
		{"an allow does not read other capitals: no rule meets, so confirm", []Rule{allowRepo}, `{"REPO":"example-org/a"}`, Confirm},
		{"an allow does not read other capitals: the next rule decides", []Rule{allowRepo, denyAll}, `{"REPO":"example-org/a"}`, Deny},
		{"the first rule that matches decides: an allow before a deny", []Rule{allowAny, denyAll}, `{"repo":"example-org/a"}`, Allow},
		{"the first rule that matches decides: a deny before an allow", []Rule{denyAll, allowAny}, `{"repo":"example-org/a"}`, Deny},
	} {
		if got := Decide(c.rules, Manifest{}, "github", "github_comment", json.RawMessage(c.args)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// A rule may spell a field's name in other capitals than the tool does. A Deny or a Confirm reads names without regard
// to capitals on both sides, so such a rule still hits the call; an Allow reads exact names, so it does not.
func TestARuleKeyInOtherCapitalsStillHitsADenyOrConfirmButNotAnAllow(t *testing.T) {
	denyOther := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"Repo": "other-org/other"}, Decision: Deny}
	confirmOther := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"Repo": "other-org/other"}, Decision: Confirm}
	allowRepo := Rule{Connector: "github", Action: "github_comment", Match: map[string]any{"Repo": "example-org/*"}, Decision: Allow}
	allowAny := Rule{Connector: "github", Action: "github_comment", Decision: Allow}
	denyAll := Rule{Connector: "github", Action: "*", Decision: Deny}
	for _, c := range []struct {
		name  string
		rules []Rule
		args  string
		want  string
	}{
		{"a deny, the call spells the name another way", []Rule{denyOther, allowAny}, `{"repo":"other-org/other"}`, Deny},
		{"a deny, the call spells the name the way the rule does", []Rule{denyOther, allowAny}, `{"Repo":"other-org/other"}`, Deny},
		{"a deny, the value is not met", []Rule{denyOther, allowAny}, `{"repo":"example-org/a"}`, Allow},
		{"a confirm, the call spells the name another way", []Rule{confirmOther, denyAll}, `{"repo":"other-org/other"}`, Confirm},
		{"a confirm, the value is not met", []Rule{confirmOther, denyAll}, `{"repo":"example-org/a"}`, Deny},
		{"an allow does not hit: no rule meets, so confirm", []Rule{allowRepo}, `{"repo":"example-org/a"}`, Confirm},
		{"an allow does not hit: the next rule decides", []Rule{allowRepo, denyAll}, `{"repo":"example-org/a"}`, Deny},
		{"an allow hits when the call spells the name the way the rule does", []Rule{allowRepo, denyAll}, `{"Repo":"example-org/a"}`, Allow},
	} {
		if got := Decide(c.rules, Manifest{}, "github", "github_comment", json.RawMessage(c.args)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// The refusal names the field as it was first written, and a name too long for a message is cut at 60 characters (not
// bytes) with an ellipsis; one of exactly 60 is whole.
func TestALongFieldNameIsCutInTheRefusal(t *testing.T) {
	ctx := context.Background()
	c := guarded{&fake{id: "example", status: Status{State: StateOK}}}
	r := NewRegistry(nil)
	_ = r.Add(c)
	for _, tc := range []struct {
		name, field, upper, shown string
	}{
		{"60 letters are whole", strings.Repeat("a", 60), strings.Repeat("A", 60), strings.Repeat("a", 60)},
		{"61 letters are cut", strings.Repeat("a", 61), strings.Repeat("A", 61), strings.Repeat("a", 60) + "…"},
		{"a long name of letters wider than a byte is cut by characters", strings.Repeat("é", 70), strings.Repeat("É", 70), strings.Repeat("é", 60) + "…"},
	} {
		args := json.RawMessage(`{"` + tc.field + `":1,"` + tc.upper + `":2}`)
		res := r.Call(ctx, "example_note", args, nil)
		if !res.IsError || !contains(res, `"`+tc.shown+`"`) || contains(res, tc.field+`"`) && tc.shown != tc.field {
			t.Errorf("%s: %+v", tc.name, res)
		}
	}
}

// The names the gate counts as one are the names encoding/json counts as one: a pair it folds together is refused as a
// field given twice, and a pair it keeps apart is not (refusing more would be harmless, refusing less is the hole).
func TestTwoSpellingsAreWhatTheDecoderFolds(t *testing.T) {
	for _, pair := range [][2]string{
		{"repo", "REPO"}, {"repo", "Repo"}, {"repo", "rEpO"}, {"repo", "repo"},
		{"key", "\u212aey"}, {"set", "\u017fet"}, {"\u212aey", "\u212aEY"},
		{"id", "\u0131d"}, {"id", "\u0130d"}, {"id", "I\u0307d"}, {"repo", "\u0440epo"}, {"repo", "repo "}, {"repo", "re_po"},
	} {
		a, b := pair[0], pair[1]
		var one, other string
		_ = json.Unmarshal([]byte(`"`+a+`"`), &one)
		_ = json.Unmarshal([]byte(`"`+b+`"`), &other)
		// the connector's side: a struct field named one, handed a call that names other
		typ := reflect.StructOf([]reflect.StructField{{Name: "F", Type: reflect.TypeOf(""), Tag: reflect.StructTag(`json:"` + one + `"`)}})
		got := reflect.New(typ)
		if err := json.Unmarshal([]byte(`{"`+b+`":"v"}`), got.Interface()); err != nil {
			t.Fatalf("%q: %v", pair, err)
		}
		folded := got.Elem().Field(0).String() == "v"
		args := json.RawMessage(`{"` + a + `":"x","` + b + `":"y"}`)
		want := Allow
		if folded {
			want = Deny
		}
		if d := Decide([]Rule{{Connector: "*", Action: "*", Decision: Allow}}, Manifest{}, "github", "github_comment", args); d != want {
			t.Errorf("%q and %q: %s, but the decoder %s them (%s)", one, other, d, map[bool]string{true: "folds", false: "keeps apart"}[folded], args)
		}
	}
}

// A hub.json rule that allows everything cannot make the AI write what the manifest says to always confirm: the card
// is still asked, and a decline stops it. The user's own write (Run) is not gated, so it is not asked.
func TestConfirmAlwaysStillAsks(t *testing.T) {
	ctx := context.Background()
	asked := 0
	no := func(context.Context, string, json.RawMessage) bool { asked++; return false }
	yes := func(context.Context, string, json.RawMessage) bool { asked++; return true }
	c := guarded{&fake{id: "example", status: Status{State: StateOK}}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Allow}})
	_ = r.Add(c)

	if res := r.Call(ctx, "example_comment", nil, no); !res.IsError || asked != 1 || len(c.did) != 0 {
		t.Fatalf("an allow rule ran a confirm_always action: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Call(ctx, "example_comment", nil, nil); !res.IsError || len(c.did) != 0 {
		t.Fatalf("ran with no way to ask: %+v %v", res, c.did)
	}
	if res := r.Call(ctx, "example_comment", nil, yes); res.IsError || asked != 2 || len(c.did) != 1 {
		t.Fatalf("confirmed but did not run: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Call(ctx, "example_note", nil, no); res.IsError || asked != 2 || len(c.did) != 2 {
		t.Fatalf("an action not in confirm_always lost its allow rule: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Run(ctx, "example_comment", nil); res.IsError || asked != 2 || len(c.did) != 3 {
		t.Fatalf("the user's own write was gated: %+v, asked %d, did %v", res, asked, c.did)
	}
}

// A call marked ConfirmEach (a health conversation's, docs/assistant/health.md "Outbound") asks even where a rule allows it; a
// deny rule still refuses without asking.
func TestConfirmEachClampsAllow(t *testing.T) {
	asked := 0
	yes := func(context.Context, string, json.RawMessage) bool { asked++; return true }
	c := guarded{&fake{id: "example", status: Status{State: StateOK}}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "example_note", Decision: Allow}, {Connector: "*", Action: "*", Decision: Deny}})
	_ = r.Add(c)
	each := ConfirmEach(context.Background())
	if res := r.Call(each, "example_note", nil, yes); res.IsError || asked != 1 || len(c.did) != 1 {
		t.Fatalf("ConfirmEach did not ask: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Call(context.Background(), "example_note", nil, yes); res.IsError || asked != 1 || len(c.did) != 2 {
		t.Fatalf("without ConfirmEach the allow rule asked: %+v, asked %d", res, asked)
	}
	if res := r.Call(each, "example_comment", nil, yes); !res.IsError || asked != 1 {
		t.Fatalf("a deny rule asked or ran: %+v, asked %d", res, asked)
	}
}
