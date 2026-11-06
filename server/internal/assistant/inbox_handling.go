package assistant

import "time"

// The processing conversation bound to an event (InboxItem.Handling) and the manager's suggestion on it
// (InboxItem.Suggestion), docs/inbox.md「事件与处理链条（第一期）」.

// Handling states.
const (
	HandlingLooking = "looking" // the manager is on it (进行中, AI 正在看)
	HandlingAgent   = "agent"   // a sub-session it started is working
	HandlingWaiting = "waiting" // a confirmed action went out: waiting for the other side
	HandlingDone    = "done"    // it finished this round (a suggestion out, or a conclusion)
	HandlingFailed  = "failed"  // failed or timed out: back to 需要你 without a suggestion
)

// HandlingOwner is the only manager of phase 1.
const HandlingOwner = "manager"

// InboxHandling is the processing conversation bound to an event.
type InboxHandling struct {
	Owner     string     `json:"owner"`
	State     string     `json:"state"`
	MachineID string     `json:"machine_id,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// Suggestion kinds.
const (
	SuggestReply      = "reply"      // one message to send
	SuggestAgent      = "agent"      // an instruction for a new Agent
	SuggestConclusion = "conclusion" // done: just read it
)

// InboxSuggestion is the manager's one current suggestion on an event; Version bumps with every new one and a
// confirmation must name it.
type InboxSuggestion struct {
	Version   int         `json:"version"`
	Kind      string      `json:"kind"`
	Text      string      `json:"text"`
	Note      string      `json:"note,omitempty"`
	Handback  bool        `json:"handback"`
	MachineID string      `json:"machine_id,omitempty"`
	Changes   []DocChange `json:"changes,omitempty"`
	At        time.Time   `json:"at"`
}
