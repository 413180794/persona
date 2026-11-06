// Package procinfo reads what the system says about a process: its parent and when it started. It is shared by the
// node and its thin socket clients (internal/nodeclient), so it imports nothing of Persona.
package procinfo

import "errors"

// ErrUnknown: this system does not tell.
var ErrUnknown = errors.New("not known on this system")
