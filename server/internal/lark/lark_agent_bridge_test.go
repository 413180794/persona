package lark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type seen struct{ method, escaped, query, body, auth string }

func bridgeServer(t *testing.T, status int, reply string) (*Client, *[]seen) {
	t.Helper()
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(b), r.Header.Get("Authorization")})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"}), &got
}

const realKey = "agent:grok_bot:a6094fe692a7b:8f798b0c-5cb9-4d5f-bdf5-fde194dbe5a3/"
const escKey = "agent:grok_bot:a6094fe692a7b:8f798b0c-5cb9-4d5f-bdf5-fde194dbe5a3%2F"

func TestAgentMessagesEscapesKeyAndKeepsUnknownBlocks(t *testing.T) {
	c, got := bridgeServer(t, 200, `{"session":{"sessionKey":"k","canSend":true},"messages":[
	 {"externalId":"e1","role":"tool","blocks":[{"type":"tool-call","name":"voice-call"},{"type":"mystery","x":{"y":1}}],"position":105},
	 {"externalId":"e2","role":"user","blocks":[{"type":"text","text":"hi"}],"local":{"clientId":"c","status":"sent"}}],
	 "hasMore":true,"nextCursor":"WzE3"}`)
	page, err := c.AgentMessages(context.Background(), realKey, "WzE3", 5)
	if err != nil {
		t.Fatal(err)
	}
	if s := (*got)[0]; s.escaped != "/agent/v1/agent-sessions/"+escKey+"/messages" || s.query != "cursor=WzE3&limit=5" || s.auth != "Bearer tok" {
		t.Fatalf("request = %+v", s)
	}
	if !page.HasMore || page.NextCursor != "WzE3" || len(page.Messages) != 2 || page.Messages[1].Local.Status != "sent" {
		t.Fatalf("page = %+v", page)
	}
	b := page.Messages[0].Blocks
	if b[0].Name != "voice-call" || b[1].Type != "mystery" {
		t.Fatalf("blocks = %+v", b)
	}
	out, _ := json.Marshal(b[1])
	if string(out) != `{"type":"mystery","x":{"y":1}}` {
		t.Fatalf("unknown block re-encoded as %s", out)
	}
}

func TestAgentSendPassesClientID(t *testing.T) {
	c, got := bridgeServer(t, 200, `{"ok":true}`)
	if _, err := c.AgentSend(context.Background(), "a b/c", "cid-1", "hello"); err != nil {
		t.Fatal(err)
	}
	s := (*got)[0]
	if s.method != "POST" || s.escaped != "/agent/v1/agent-sessions/a%20b%2Fc/messages" {
		t.Fatalf("request = %+v", s)
	}
	var body map[string]string
	_ = json.Unmarshal([]byte(s.body), &body)
	if body["clientId"] != "cid-1" || body["text"] != "hello" {
		t.Fatalf("body = %s", s.body)
	}
	if _, err := c.AgentSend(context.Background(), "k", "", "x"); err == nil {
		t.Fatal("empty clientId accepted")
	}
}

func TestAgentDeleteAndRead(t *testing.T) {
	c, got := bridgeServer(t, 200, ``)
	if err := c.AgentDeleteMessage(context.Background(), "k/1", "c/2"); err != nil {
		t.Fatal(err)
	}
	if err := c.AgentMarkRead(context.Background(), "k/1"); err != nil {
		t.Fatal(err)
	}
	if (*got)[0].method != "DELETE" || (*got)[0].escaped != "/agent/v1/agent-sessions/k%2F1/messages/c%2F2" ||
		(*got)[1].method != "POST" || (*got)[1].escaped != "/agent/v1/agent-sessions/k%2F1/read" {
		t.Fatalf("requests = %+v", *got)
	}
}

func TestAgentSendReadOnlyTokenIsForbidden(t *testing.T) {
	c, _ := bridgeServer(t, 403, `{"error":{"code":"AGENT_READ_ONLY","message":"read-only token"}}`)
	_, err := c.AgentSend(context.Background(), "k", "c", "x")
	var ae *APIError
	if !errors.Is(err, ErrForbidden) || !errors.As(err, &ae) || ae.Code != "AGENT_READ_ONLY" {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentErrorsKeepStatus(t *testing.T) {
	c, _ := bridgeServer(t, 404, `{"error":{"code":"NOT_FOUND","message":"no session"}}`)
	_, err := c.AgentSessions(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("err = %v", err)
	}
	c, _ = bridgeServer(t, 401, `{"error":{"code":"AGENT_TOKEN_INVALID","message":"bad"}}`)
	if _, err := c.AgentBridgeStatus(context.Background()); err == nil {
		t.Fatal("401 accepted")
	}
}

func TestAgentListCalls(t *testing.T) {
	c, got := bridgeServer(t, 200, `{"enabled":true,"providers":[{"provider":"cue","connection":"server","available":true}],
	 "accounts":[{"accountId":"a","provider":"grok_bot","status":"active","sessionCount":68}],
	 "sessions":[{"sessionKey":"agent:muse:x:main","provider":"muse","canSend":true,"unreadCount":2,"rankTimeMs":5}],
	 "items":[{"sessionKey":"s","text":"t","role":"assistant"}]}`)
	ctx := context.Background()
	st, err := c.AgentBridgeStatus(ctx)
	if err != nil || !st.Enabled || st.Accounts[0].SessionCount != 68 {
		t.Fatalf("status %+v %v", st, err)
	}
	ss, err := c.AgentSessions(ctx)
	if err != nil || ss[0].Provider != "muse" || ss[0].UnreadCount != 2 {
		t.Fatalf("sessions %+v %v", ss, err)
	}
	hits, err := c.AgentSessionSearch(ctx, "a&b")
	if err != nil || hits[0].Role != "assistant" {
		t.Fatalf("search %+v %v", hits, err)
	}
	feed, err := c.AgentBridgeFeed(ctx)
	if err != nil || len(feed.Items) != 1 {
		t.Fatalf("feed %+v %v", feed, err)
	}
	if (*got)[2].query != "q=a%26b" {
		t.Fatalf("search query %q", (*got)[2].query)
	}
}

func TestParseAgentBridgeUpdated(t *testing.T) {
	ev, ok := parseEvent([]byte(`{"type":"agent-bridge.updated","sessionKeys":["k1","k2"]}`))
	if !ok || ev.Type != "agent-bridge.updated" || len(ev.SessionKeys) != 2 || len(ev.FeedIDs) != 0 {
		t.Fatalf("ev = %+v ok=%v", ev, ok)
	}
}

func TestRawStillReadOnlyForAgentSessions(t *testing.T) {
	if readOnly("POST", "/agent-sessions/k/messages") == nil || readOnly("DELETE", "/agent-sessions/k/messages/c") == nil {
		t.Fatal("raw gate loosened")
	}
}
