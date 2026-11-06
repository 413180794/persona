package node

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A background task's notification starts a turn of its own; its result must not leave the conversation marked
// finished while claude goes on with the message queued behind it.
func TestAssistantBusyAcrossQueuedTurns(t *testing.T) {
	n := assistantNode(t)
	p := &assistantProc{id: "11111111-2222-3333-4444-555555555555", busy: true}
	busy := func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.busy }
	stream := func(lines ...string) { n.readAssistant(p, strings.NewReader(strings.Join(lines, "\n")+"\n")) }

	stream(`{"type":"stream_event","event":{"type":"message_start"}}`, `{"type":"result","subtype":"success"}`)
	if busy() {
		t.Fatal("a finished turn should clear busy")
	}
	stream(`{"type":"stream_event","event":{"type":"message_start"}}`)
	if !busy() {
		t.Fatal("a turn claude starts on its own (the queued message) should mark the conversation busy again")
	}
	stream(`{"type":"result","subtype":"success"}`)
	if busy() {
		t.Fatal("its result should clear busy")
	}
}

// A subagent's stream interleaved with the conversation's (#4495): its deltas are not the conversation's live text,
// its message_start does not clear the conversation's text or turn error, and its message_stop is not what the
// conversation just said.
func TestAssistantIgnoresSubagentStreamEvents(t *testing.T) {
	n := assistantNode(t)
	all, pw := hubPushes(n)
	t.Cleanup(func() { pw.Close() })
	const id = "11111111-2222-3333-4444-555555555555"
	p := &assistantProc{id: id}
	main := func(e string) string { return `{"type":"stream_event","event":` + e + `,"parent_tool_use_id":null}` }
	sub := func(e string) string {
		return `{"type":"stream_event","event":` + e + `,"parent_tool_use_id":"toolu_sub"}`
	}
	delta := func(s string) string {
		return `{"type":"content_block_delta","delta":{"type":"text_delta","text":"` + s + `"}}`
	}
	n.readAssistant(p, strings.NewReader(strings.Join([]string{
		main(`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`),
		main(delta("Looking into it")),
	}, "\n")+"\n"))
	p.mu.Lock()
	p.turnError, p.lastDelta = "rate limited", time.Time{}
	p.mu.Unlock()
	n.readAssistant(p, strings.NewReader(strings.Join([]string{
		sub(`{"type":"message_start","message":{"usage":{"input_tokens":90000}}}`),
		sub(delta("SUBAGENT WORDS")),
		sub(`{"type":"message_stop"}`),
	}, "\n")+"\n"))
	p.mu.Lock()
	partial, turnError, context := p.partial.String(), p.turnError, p.context
	p.mu.Unlock()
	if partial != "Looking into it" {
		t.Errorf("the conversation's live text is %q", partial)
	}
	if turnError != "rate limited" {
		t.Errorf("a subagent's start cleared the turn error: %q", turnError)
	}
	if context != 5 {
		t.Errorf("context %d, want the conversation's own 5", context)
	}
	// The conversation's own stop still says what it said.
	n.readAssistant(p, strings.NewReader(main(`{"type":"message_stop"}`)+"\n"))
	// hubPushes never closes its channel: read until it has been quiet for a moment.
	var texts []string
	for done := false; !done; {
		select {
		case m := <-all:
			if m.Event != "session.delta" {
				continue
			}
			var d struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(m.Params, &d)
			texts = append(texts, d.Text)
		case <-time.After(300 * time.Millisecond):
			done = true
		}
	}
	for _, s := range texts {
		if strings.Contains(s, "SUBAGENT") {
			t.Fatalf("a subagent's words went out as the conversation's: %q", texts)
		}
	}
	if len(texts) == 0 || texts[len(texts)-1] != "Looking into it" {
		t.Fatalf("live texts %q, want the conversation's own last", texts)
	}
}
