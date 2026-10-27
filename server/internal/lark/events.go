package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Event is the part of an lark-connector realtime event the hub relays: which conversations changed.
type Event struct {
	Type    string   // message.upserted, conversation.upserted, …
	FeedIDs []string // conversations it touches
}

// Events follows lark-connector's realtime stream (GET /agent/v1/events, a WebSocket of {"type":…} events) until ctx ends,
// reconnecting with backoff, and hands each event that concerns conversations or their messages to fn.
func (c *Client) Events(ctx context.Context, fn func(Event), logf func(string, ...any)) {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := c.followEvents(ctx, fn)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		logf("lark events: %v; reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func (c *Client) followEvents(ctx context.Context, fn func(Event)) error {
	tok, err := c.agentToken(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimSuffix(c.cfg.BaseURL, "/") + "/events"
	u = "ws" + strings.TrimPrefix(u, "http") // http → ws, https → wss
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tok}}})
	cancel()
	if err != nil {
		return err
	}
	defer conn.CloseNow()
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
	}
	if json.Unmarshal(data, &raw) != nil {
		return Event{}, false
	}
	switch raw.Type {
	case "message.upserted", "message.deleted", "message.recalled", "conversation.upserted", "conversations.upserted",
		"feed.settings.updated":
	default:
		return Event{}, false // presence, typing, todo, mail, meetings…
	}
	ev := Event{Type: raw.Type}
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
