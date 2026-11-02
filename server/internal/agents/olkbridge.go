package agents

import (
	"context"
	"encoding/json"

	"github.com/chyroc/jarvis/server/internal/lark"
)

const OLKBridge = "olk-bridge"

// BridgeAPI is the part of *lark.Client the olk-bridge source uses.
type BridgeAPI interface {
	AgentBridgeStatus(ctx context.Context) (*lark.AgentBridgeState, error)
	AgentBridgeFeed(ctx context.Context) (*lark.AgentFeed, error)
	AgentSessions(ctx context.Context) ([]lark.AgentSession, error)
	AgentSessionSearch(ctx context.Context, q string) ([]lark.AgentSearchHit, error)
	AgentMessages(ctx context.Context, key, cursor string, limit int) (*lark.AgentMessagePage, error)
	AgentSend(ctx context.Context, key, clientID, text string) (json.RawMessage, error)
	AgentDeleteMessage(ctx context.Context, key, clientID string) error
	AgentMarkRead(ctx context.Context, key string) error
}

// Bridge is lark-connector's agent-bridge as a Source. Providers: cue, grok_bot (server), muse, dots, claude_duet (browser).
// Only the hub's one configured lark-connector account is queried: the bridge's state is deployment-wide (verified: account 1
// sees all sessions; other selectors are rejected), so querying more accounts would only repeat it.
type Bridge struct{ API BridgeAPI }

func (b Bridge) Name() string { return OLKBridge }

func wrap(s lark.AgentSession) Session {
	return Session{ID: OLKBridge + ":" + s.SessionKey, Source: OLKBridge, AgentSession: s}
}

func (b Bridge) Status(ctx context.Context) (Status, error) {
	st, err := b.API.AgentBridgeStatus(ctx)
	if err != nil {
		return Status{Source: OLKBridge, Error: err.Error()}, err
	}
	return Status{Source: OLKBridge, Enabled: st.Enabled, Providers: st.Providers, Accounts: st.Accounts}, nil
}

func (b Bridge) Sessions(ctx context.Context) ([]Session, error) {
	ss, err := b.API.AgentSessions(ctx)
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
func (b Bridge) avatars(ctx context.Context) map[string]string {
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

func (b Bridge) Search(ctx context.Context, q string) ([]SearchHit, error) {
	hits, err := b.API.AgentSessionSearch(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, len(hits))
	for i, h := range hits {
		out[i] = SearchHit{ID: OLKBridge + ":" + h.SessionKey, Source: OLKBridge, AgentSearchHit: h}
	}
	return out, nil
}

func (b Bridge) Messages(ctx context.Context, key, cursor string, limit int) (*MessagePage, error) {
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

func (b Bridge) Send(ctx context.Context, key, clientID, text string) (json.RawMessage, error) {
	return b.API.AgentSend(ctx, key, clientID, text)
}

func (b Bridge) DeleteMessage(ctx context.Context, key, clientID string) error {
	return b.API.AgentDeleteMessage(ctx, key, clientID)
}

func (b Bridge) MarkRead(ctx context.Context, key string) error { return b.API.AgentMarkRead(ctx, key) }
