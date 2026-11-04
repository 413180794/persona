package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Event is the part of an lark-connector realtime event the hub relays: which conversations changed.
type Event struct {
	Type    string   // message.upserted, conversation.upserted, …
	FeedIDs []string // conversations it touches
	// Message is the message a message.upserted event carries (new, edited, or a thread's root updated).
	Message *Message
	// SessionKeys are the agent-bridge sessions an agent-bridge.updated event touches.
	SessionKeys []string
}

// Events follows lark-connector's realtime stream (GET /agent/v1/events, a WebSocket of {"type":…} events) until ctx ends,
// reconnecting with backoff, and hands each event that concerns conversations or their messages to fn.
//
// While lark-connector is logged out of Feishu (ErrNotLoggedIn) the loop keeps retrying at the capped backoff with the
// same token, so it reconnects by itself within a minute of the user logging in again; the same failure is
// logged once, not on every retry.
func (c *Client) Events(ctx context.Context, fn func(Event), logf func(string, ...any)) {
	backoff := eventsBackoff
	lastErr := ""
	for ctx.Err() == nil {
		start := time.Now()
		err := c.followEvents(ctx, fn, func() {
			if lastErr != "" {
				logf("lark events: connected")
				lastErr = ""
			}
		})
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > time.Minute {
			backoff = eventsBackoff
		}
		if msg := err.Error(); msg != lastErr {
			logf("lark events: %v; retrying (backoff up to %s)", err, eventsMaxBackoff)
			lastErr = msg
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, eventsMaxBackoff)
	}
}

// Reconnect backoff of the realtime stream (variables so tests can shorten them).
var eventsBackoff, eventsMaxBackoff = time.Second, time.Minute

func (c *Client) followEvents(ctx context.Context, fn func(Event), connected func()) error {
	tok, err := c.agentToken(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimSuffix(c.cfg.BaseURL, "/") + "/events"
	u = "ws" + strings.TrimPrefix(u, "http") // http → ws, https → wss
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": {"Bearer " + tok}, "X-lark-connector-Account": {strconv.Itoa(c.Account())},
	}})
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			var raw []byte
			if resp.Body != nil { // the library keeps the first KB of a failed handshake's body
				raw, _ = io.ReadAll(resp.Body)
			}
			return c.unauthorized(raw)
		}
		return err
	}
	defer conn.CloseNow()
	connected()
	conn.SetReadLimit(32 << 20) // a message event carries the whole message
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if ev, ok := parseEvent(data); ok {
			fn(ev)
		}
	}
}

func parseEvent(data []byte) (Event, bool) {
	var raw struct {
		Type          string `json:"type"`
		FeedID        string `json:"feedId"`
		Conversations []struct {
			FeedID string `json:"feedId"`
		} `json:"conversations"`
		Message     *rawMessage `json:"message"`
		SessionKeys []string    `json:"sessionKeys"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return Event{}, false
	}
	switch raw.Type {
	case "message.upserted", "message.deleted", "message.recalled", "conversation.upserted", "conversations.upserted",
		"feed.settings.updated", "local.settings.updated":
	case "agent-bridge.updated":
		return Event{Type: raw.Type, SessionKeys: raw.SessionKeys}, true
	default:
		return Event{}, false // presence, typing, todo, mail, meetings…
	}
	ev := Event{Type: raw.Type}
	if raw.Type == "message.upserted" && raw.Message != nil && raw.Message.ID != "" {
		m := raw.Message.normalize()
		ev.Message = &m
	}
	if raw.FeedID != "" {
		ev.FeedIDs = append(ev.FeedIDs, raw.FeedID)
	}
	for _, c := range raw.Conversations {
		if c.FeedID != "" {
			ev.FeedIDs = append(ev.FeedIDs, c.FeedID)
		}
	}
	return ev, true
}
