package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/rpc"
)

// What comes in from the channels for the assistant (assistant_channels.go): the owner's messages and card presses.
// The transport took them, so none is dropped without a word: one waits for the assistant (the first seconds of a hub
// start, an assistant restarting) and is asked again when a call was cut off or timed out — the assistant takes a
// message once by its id, so asking again is safe — and only when the assistant stays away is the owner told it can't
// be reached. What the owner did is handed over with a single-use owner action: answering a card in a channel needs
// one (interactions.resolve), so the assistant can't approve a plan, allow a tool or type into a terminal by itself.

// inboundWait is how long an owner's message waits for the assistant (tests shorten it): longer than a hub start
// or an assistant restart takes.
var inboundWait = 60 * time.Second

// inboundMost is how many messages may wait at once (a plugin delivering its backlog right as the hub starts);
// beyond it the owner is told at once.
const inboundMost = 500

// inbound is what waits for the assistant, and the owner actions handed over.
type inbound struct {
	waiting atomic.Int32
	mu      sync.Mutex
	actions map[string]ownerAction
}

// ownerAction is an owner's press or message the hub handed the assistant: what it may answer with it.
type ownerAction struct {
	channel     string
	interaction string // a card's press: that card only ("" for a message: what the channel asked last)
	expires     time.Time
}

const ownerActionFor = 10 * time.Minute

// issueOwnerAction records an action of the owner's in channel (for interaction when it is a card's press) and
// returns its id for the assistant.
func (h *Hub) issueOwnerAction(channel, interaction string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	id := "oa_" + hex.EncodeToString(b[:])
	in := &h.chans.in
	in.mu.Lock()
	defer in.mu.Unlock()
	now := time.Now()
	for k, a := range in.actions {
		if now.After(a.expires) {
			delete(in.actions, k)
		}
	}
	if in.actions == nil {
		in.actions = map[string]ownerAction{}
	}
	in.actions[id] = ownerAction{channel: channel, interaction: interaction, expires: now.Add(ownerActionFor)}
	return id
}

// errNoOwnerAction: an answer to a card from a channel without the owner's action behind it.
var errNoOwnerAction = errors.New("这个回答没有对应主人在渠道里的操作：批准类的不能代答")

// takeOwnerAction uses up the owner action id for an answer to interaction (once).
func (h *Hub) takeOwnerAction(id, interaction string) error {
	in := &h.chans.in
	in.mu.Lock()
	defer in.mu.Unlock()
	a, ok := in.actions[id]
	if !ok || id == "" || time.Now().After(a.expires) {
		return errNoOwnerAction
	}
	if a.interaction != "" && a.interaction != interaction {
		return errNoOwnerAction
	}
	delete(in.actions, id)
	return nil
}

// toAssistant hands one of the owner's messages to the assistant (method, params): it waits for the assistant up to
// inboundWait and asks again after a cut-off or timed-out call. An error is the assistant's own refusal, or
// errAssistantOffline when it could not be reached in time.
func (h *Hub) toAssistant(method string, params any) error {
	in := &h.chans.in
	wait := inboundWait
	if in.waiting.Add(1) > inboundMost {
		wait = 0 // too many waiting already: say so at once
	}
	defer in.waiting.Add(-1)
	return h.callAssistantWaiting(method, params, nil, time.Now().Add(wait), 20*time.Second)
}

// callAssistantWaiting calls the assistant until deadline: while it is not connected it waits, a call cut off or out
// of time (each one at most per) goes again.
func (h *Hub) callAssistantWaiting(method string, params, out any, deadline time.Time, per time.Duration) error {
	for {
		if h.busConnOf(assistantwire.AssistantID) == nil {
			if time.Now().After(deadline) {
				return errAssistantOffline
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		left := time.Until(deadline)
		ctx, cancel := context.WithTimeout(context.Background(), max(min(per, left), time.Second))
		err := h.callBus(ctx, assistantwire.AssistantID, method, params, out)
		cancel()
		var re *rpc.Error
		switch {
		case err == nil:
			return nil
		case errors.As(err, &re) && re.Code != bus.CodeStaleConnection && re.Code != assistantwire.CodeTryAgain:
			return err // the assistant's answer: it refused this one
		case time.Now().After(deadline):
			return errAssistantOffline
		}
		time.Sleep(200 * time.Millisecond)
	}
}
