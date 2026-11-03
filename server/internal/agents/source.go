// Package agents is Jarvis' single view of the agents it can reach: lark-connector's paired personal agents
// (agent-bridge, source "removedbridge") now; Jarvis' own /agent and OLAP may join as further Sources.
// A session's public id is "<source>:<sessionKey>" (lark-connector's own keys already start with "agent:").
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/nwverse/persona/server/internal/lark"
)

// Unified models: the bridge's shapes are already the right ones, so they are reused, with the public id added.
type (
	Block   = lark.AgentBlock
	Message = lark.AgentMessage
)

type Session struct {
	ID     string `json:"id"` // <source>:<sessionKey>
	Source string `json:"source"`
	lark.AgentSession
	// AvatarURL is lark-connector's avatar (data: or https URL); only its presence and version go to the apps, which load
	// the image from /api/v1/agents/sessions/{id}/avatar.
	AvatarURL     string `json:"-"`
	HasAvatar     bool   `json:"hasAvatar,omitempty"`
	AvatarVersion string `json:"avatarVersion,omitempty"`
}

type MessagePage struct {
	Session    *Session  `json:"session,omitempty"`
	Messages   []Message `json:"messages"`
	HasMore    bool      `json:"hasMore"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type SearchHit struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	lark.AgentSearchHit
}

// Status is one source's health and the agents it serves.
type Status struct {
	Source    string               `json:"source"`
	Enabled   bool                 `json:"enabled"`
	Error     string               `json:"error,omitempty"`
	Providers []lark.AgentProvider `json:"providers"`
	Accounts  []lark.AgentAccount  `json:"accounts"`
}

// Source is one backend of agents and their sessions; keys are the source's own session keys.
type Source interface {
	Name() string
	Status(ctx context.Context) (Status, error)
	Sessions(ctx context.Context) ([]Session, error)
	Search(ctx context.Context, q string) ([]SearchHit, error)
	Messages(ctx context.Context, key, cursor string, limit int) (*MessagePage, error)
	Send(ctx context.Context, key, clientID, text string) (json.RawMessage, error)
	DeleteMessage(ctx context.Context, key, clientID string) error
	MarkRead(ctx context.Context, key string) error
}

var ErrBadID = errors.New("agents: session id must be <source>:<sessionKey>")

// SplitID splits a public id into source name and session key.
func SplitID(id string) (source, key string, err error) {
	source, key, ok := strings.Cut(id, ":")
	if !ok || source == "" || key == "" {
		return "", "", ErrBadID
	}
	return source, key, nil
}
