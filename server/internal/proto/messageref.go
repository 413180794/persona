package proto

import "strings"

// MessageRefMost is the most of a message ref, the id of a message Persona sent as the user on the other side, that the
// sent-as-user ledger keeps, in characters (docs/extensions/connectors.md "Sent as the user").
const MessageRefMost = 200

// CleanMessageRef is a message ref as the ledger keeps it: an id of the service's own form, so it stays as it is, only
// trimmed and without NUL and invalid UTF-8 (Postgres refuses a row that has them). One longer than MessageRefMost
// characters once cleaned is "": a cut id would point at nothing, or at another message, and a recall acts on it. The
// node's gate proxy, the manager's three sources of a row (the gate, sent.record and the node's report) all read a ref
// through this, so that they agree.
func CleanMessageRef(s string) string {
	s = strings.TrimSpace(strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), ""))
	if len([]rune(s)) > MessageRefMost {
		return ""
	}
	return s
}
