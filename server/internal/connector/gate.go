package connector

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
)

// Decisions of the write gate.
const (
	Allow   = "allow"   // run it
	Confirm = "confirm" // the user allows it on the phone first (the default)
	Deny    = "deny"    // refuse, and tell the model why
)

// Rule is one rule of the write gate (hub.json connector_gate): an action of a connector — both path.Match
// patterns, "*" for any — whose arguments' top-level fields equal Match when it is set (a string value is a pattern
// too) gets Decision. The first matching rule decides; when none matches, the user confirms.
type Rule struct {
	Connector string         `json:"connector"`
	Action    string         `json:"action"`
	Match     map[string]any `json:"match,omitempty"`
	Decision  string         `json:"decision"`
}

// ValidateRules reports a rule the gate could not use, so a typo in hub.json stops the start instead of quietly
// asking (or worse, quietly allowing) something else.
func ValidateRules(rules []Rule) error {
	for i, r := range rules {
		switch r.Decision {
		case Allow, Confirm, Deny:
		default:
			return fmt.Errorf("connector_gate[%d]: decision %q is not allow, confirm or deny", i, r.Decision)
		}
		for _, p := range []string{r.Connector, r.Action} {
			if _, err := path.Match(p, ""); err != nil || p == "" {
				return fmt.Errorf("connector_gate[%d]: bad pattern %q", i, p)
			}
		}
	}
	return nil
}

// Decide is what the gate says about the action name of connector id with args.
func Decide(rules []Rule, id, name string, args json.RawMessage) string {
	var fields map[string]any
	_ = json.Unmarshal(args, &fields)
	for _, r := range rules {
		if matches(r.Connector, id) && matches(r.Action, name) && matchFields(r.Match, fields) {
			return r.Decision
		}
	}
	return Confirm
}

func matches(pattern, s string) bool {
	ok, err := path.Match(pattern, s)
	return err == nil && ok
}

func matchFields(want, got map[string]any) bool {
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			return false
		}
		if ws, isStr := w.(string); isStr {
			if gs, ok := g.(string); !ok || !matches(ws, gs) {
				return false
			}
		} else if !reflect.DeepEqual(w, g) {
			return false
		}
	}
	return true
}
