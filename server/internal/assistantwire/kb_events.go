package assistantwire

const (
	// EventKBChanged travels from the assistant to the hub; extensions receive event.kb.changed with kb.read.
	EventKBChanged = "kb.changed"
	// EventKBForgotten is sent after a source's knowledge-base files and index rows have been removed.
	EventKBForgotten = "kb.forgotten"
)

// KBChanged is an invalidation hint, never entity contents, paths, titles or raw identities.
type KBChanged struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	What string `json:"what"`
}

type KBForgottenEvent struct {
	Source string `json:"source"`
}
