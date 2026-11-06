package connectorwire

import (
	"time"
)

// SnapshotData is a connector's live state. Nil parts are what it does not know.
type SnapshotData struct {
	Chats *SnapshotChats `json:"chats,omitempty"`
	// Meetings are the rest of today's (in tz), not declined, sorted by start.
	Meetings []SnapshotMeeting `json:"meetings,omitempty"`
}

// SnapshotChats are the unread chats: those that @ the user (with the newest text's start), and the others.
type SnapshotChats struct {
	AtMe  []SnapshotChat `json:"at_me"`
	Other []SnapshotChat `json:"other"`
}

type SnapshotChat struct {
	Name    string `json:"name"`
	Unread  int    `json:"unread"`
	Preview string `json:"preview,omitempty"`
}

type SnapshotMeeting struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Title string    `json:"title"`
}
