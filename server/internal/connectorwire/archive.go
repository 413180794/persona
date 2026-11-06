package connectorwire

import "github.com/nwverse/persona/server/internal/kbwire"

// ChatBatch is one conversation's new messages, oldest first.
type ChatBatch struct {
	// Source is the name the conversation is filed under: "" is the connector's id, else one of its manifest's
	// ArchiveSources (the chat is "<source>-<chat id>").
	Source   string                 `json:"source,omitempty"`
	Chat     kbwire.SourceChat      `json:"chat,omitempty"`
	Messages []kbwire.SourceMessage `json:"messages,omitempty"`
}

// ArchiveMediaMost is the largest attachment archive.media hands over.
const ArchiveMediaMost = 20 << 20

// Media is one media's bytes and what they are.
type Media struct {
	Mime string `json:"mime"`
	Data []byte `json:"data"`
}
