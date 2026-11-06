package assistantwire

import "github.com/nwverse/persona/server/internal/kbwire"

// Extension-facing knowledge-base calls. The hub checks the extension's granted scope before forwarding these.
const (
	MethodKBList   = "kb.list"
	MethodKBGet    = "kb.get"
	MethodKBDay    = "kb.day"
	MethodKBSearch = "kb.search"
	MethodKBUpdate = "kb.update"
)

type KBListParams struct {
	Kind   string `json:"kind"`
	Tag    string `json:"tag,omitempty"`
	Limit  int    `json:"limit,omitempty"`  // default 30, at most 200
	Offset int    `json:"offset,omitempty"` // default 0; pages reflect the current ranking, without a snapshot
}

type KBListResult struct {
	Items      []kbwire.Entry `json:"items"`
	Tags       []string       `json:"tags"`
	Total      int            `json:"total"`
	NextOffset *int           `json:"next_offset,omitempty"`
}

type KBGetParams struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	RecentChars *int   `json:"recent_chars,omitempty"` // default 12000; 0 omits recent messages; at most 20000
}

type KBGetResult struct {
	Detail *kbwire.Detail `json:"detail"`
}

type KBDayParams struct {
	ID  string `json:"id"`
	Day string `json:"day"` // YYYY-MM-DD in the knowledge base's time zone
}

type KBDayResult struct {
	Day      string              `json:"day"`
	Messages []kbwire.DayMessage `json:"messages"` // n is the first source-file line, 1-based
	Prev     string              `json:"prev"`
	Next     string              `json:"next"`
}

type KBSearchParams struct {
	Query    string `json:"query"`
	Messages *bool  `json:"messages,omitempty"` // default true
	Limit    int    `json:"limit,omitempty"`    // default 40, at most 200
}

type KBSearchResult struct {
	Hits []kbwire.Hit `json:"hits"`
}

type KBUpdateParams struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	kbwire.Update
}

type KBUpdateResult struct {
	ID string `json:"id"`
}
