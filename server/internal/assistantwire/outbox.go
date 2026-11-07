package assistantwire

import (
	"encoding/json"
	"time"
)

// MethodOutbox: Outbox → OutboxResult. A manager hands the hub, once it is back, the pushes and the apps' live
// updates it made while the hub was away (docs/operations/hub-restart.md "The manager while the hub is away"), oldest first. An
// older hub answers it unknown_method: the manager then sends them one by one (notify.request, ws.broadcast).
const MethodOutbox = "manager.outbox"

// Outbox is manager.outbox's params.
type Outbox struct {
	Items []OutboxItem `json:"items"`
}

// OutboxItem is one of them, made At: a push (Notification, as notify.request carries it) or an apps' live update
// (Message, as ws.broadcast carries it). The hub takes each ID once, however often it comes (a manager that did not
// hear the answer sends it again).
type OutboxItem struct {
	ID           string          `json:"id"`
	At           time.Time       `json:"at"`
	Notification *Notification   `json:"notification,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
}

// OutboxResult are the ids the hub took (now, or before): the manager forgets them.
type OutboxResult struct {
	Done []string `json:"done"`
}
