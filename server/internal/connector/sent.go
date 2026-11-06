package connector

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// How the gate let a write through (LetThrough.Kind).
const (
	ThroughRule = "rule" // a connector_gate rule allowed it
	ThroughCard = "card" // the user allowed it on the phone
)

// LetThrough is how the gate let a write through.
type LetThrough struct {
	Kind string
	// Rule is the rule that allowed it (ThroughRule).
	Rule *Rule
	// Summary is the words the card showed (ThroughCard): Describe's, cut to cardMost.
	Summary string
}

// Sent is a write that went out as the user: the gate let it through and the action ran without an error. A write the
// gate refused or the user declined, one that failed, and one the user sent by their own hand (Run) are not.
type Sent struct {
	At        time.Time
	Connector string          // the connector's id
	Action    string          // the tool's name
	Args      json.RawMessage // what the action was given
	Text      string          // what it wrote: the arguments' body / text / message…, cut to SentTextMost characters
	Result    string          // what it answered
	Through   LetThrough
}

// SentTextMost is the most of a write's text that Sent keeps, in characters.
const SentTextMost = 4000

// writtenText is the words a write puts out, as its arguments say them: the first of the fields connectors name
// their text by (GitHub's body, mail's and the chats' text) that has any, cut to SentTextMost characters.
func writtenText(args json.RawMessage) string {
	var fields map[string]any
	_ = json.Unmarshal(args, &fields)
	for _, k := range []string{"body", "text", "message", "content", "comment"} {
		if s, _ := fields[k].(string); strings.TrimSpace(s) != "" {
			if r := []rune(s); len(r) > SentTextMost {
				return string(r[:SentTextMost])
			}
			return s
		}
	}
	return ""
}

// SetSent sets what is told about every write that went out through Call (nil: nothing). It is called once the write
// is done, in the call's goroutine and with its ctx, so keep it quick: what it does cannot undo the write, and its
// failure is its own to deal with.
func (r *Registry) SetSent(f func(ctx context.Context, s Sent)) {
	r.mu.Lock()
	r.sent = f
	r.mu.Unlock()
}
