package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// testBridge is a source over a fake API with the shape the Lark gateway's agent-bridge had (the hub's own source before a
// plugin served it): the service's tests go through it.

type AgentBridgeState struct {
	Enabled   bool            `json:"enabled"`
	Providers []AgentProvider `json:"providers"`
	Accounts  []AgentAccount  `json:"accounts"`
}

type AgentFeed struct {
	Enabled bool              `json:"enabled"`
	Items   []json.RawMessage `json:"items"`
}

type apiError struct{ Status int }

func (e *apiError) Error() string { return http.StatusText(e.Status) }

// testSource is the source name the test bridge registers under.
const testSource = "lark-bridge"

// BridgeAPI is the part of *Client the Lark plugin's source uses.
type BridgeAPI interface {
	AgentBridgeStatus(ctx context.Context) (*AgentBridgeState, error)
	AgentBridgeFeed(ctx context.Context) (*AgentFeed, error)
	AgentSessions(ctx context.Context) ([]AgentSession, error)
	AgentSessionSearch(ctx context.Context, q string) ([]AgentSearchHit, error)
	AgentMessages(ctx context.Context, key, cursor string, limit int) (*AgentMessagePage, error)
	AgentSend(ctx context.Context, key, clientID, text string) (json.RawMessage, error)
	AgentDeleteMessage(ctx context.Context, key, clientID string) error
	AgentMarkRead(ctx context.Context, key string) error
}

// Bridge is the Lark gateway's agent-bridge as a Source. Providers: cue, grok_bot (server), muse, dots, claude_duet (browser).
// Only the hub's one configured gateway account is queried: the bridge's state is deployment-wide (verified: account 1
// sees all sessions; other selectors are rejected), so querying more accounts would only repeat it.
type testBridge struct{ API BridgeAPI }

func (b testBridge) Name() string { return testSource }

func wrap(s AgentSession) Session { return Wrap(testSource, s, "") }

// missingBridge is the Lark gateway answering 404 for the bridge's own routes: a build from before agent-bridge existed.
func missingBridge(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

func (b testBridge) Status(ctx context.Context) (Status, error) {
	st, err := b.API.AgentBridgeStatus(ctx)
	if missingBridge(err) {
		return Status{Source: testSource, Setup: SetupMissing}, nil
	}
	if err != nil {
		return Status{Source: testSource, Error: err.Error()}, err
	}
	out := Status{Source: testSource, Enabled: st.Enabled, Providers: st.Providers, Accounts: st.Accounts}
	switch {
	case !st.Enabled:
		out.Setup = SetupDisabled
	case len(st.Accounts) == 0:
		out.Setup = SetupNoAccounts
	}
	return out, nil
}

// Sessions is empty, not an error, on a Lark gateway without the bridge: Status says why (SetupMissing).
func (b testBridge) Sessions(ctx context.Context) ([]Session, error) {
	ss, err := b.API.AgentSessions(ctx)
	if missingBridge(err) {
		return []Session{}, nil
	}
	if err != nil {
		return nil, err
	}
	avatars := b.avatars(ctx)
	out := make([]Session, len(ss))
	for i, s := range ss {
		out[i] = withAvatar(wrap(s), avatars[s.SessionKey])
	}
	return out, nil
}

// avatars maps sessionKey → avatarUrl from the feed (feedId is the sessionKey). The list's /agent-sessions lacks
// avatars; a feed failure only costs the avatars.
func (b testBridge) avatars(ctx context.Context) map[string]string {
	feed, err := b.API.AgentBridgeFeed(ctx)
	if err != nil || feed == nil {
		return nil
	}
	out := map[string]string{}
	for _, raw := range feed.Items {
		var it struct {
			FeedID    string `json:"feedId"`
			AvatarURL string `json:"avatarUrl"`
		}
		if json.Unmarshal(raw, &it) == nil && it.FeedID != "" && it.AvatarURL != "" {
			out[it.FeedID] = it.AvatarURL
		}
	}
	return out
}

func (b testBridge) Search(ctx context.Context, q string) ([]SearchHit, error) {
	hits, err := b.API.AgentSessionSearch(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, len(hits))
	for i, h := range hits {
		out[i] = SearchHit{ID: testSource + ":" + h.SessionKey, Source: testSource, AgentSearchHit: h}
	}
	return out, nil
}

func (b testBridge) Messages(ctx context.Context, key, cursor string, limit int) (*MessagePage, error) {
	p, err := b.API.AgentMessages(ctx, key, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := &MessagePage{Messages: p.Messages, HasMore: p.HasMore, NextCursor: p.NextCursor}
	if out.Messages == nil {
		out.Messages = []Message{}
	}
	if p.Session != nil {
		s := wrap(*p.Session)
		out.Session = &s
	}
	return out, nil
}

func (b testBridge) Send(ctx context.Context, key, clientID, text string) (json.RawMessage, error) {
	return b.API.AgentSend(ctx, key, clientID, text)
}

func (b testBridge) DeleteMessage(ctx context.Context, key, clientID string) error {
	return b.API.AgentDeleteMessage(ctx, key, clientID)
}

func (b testBridge) MarkRead(ctx context.Context, key string) error {
	return b.API.AgentMarkRead(ctx, key)
}
