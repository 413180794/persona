package extmgr

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"sync"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// This capability is bound to a single locally launched extension process.
// The sandbox receives neither the gate directory nor operations for other kinds.
type larkAdmissionLease struct {
	mu     sync.Mutex
	gate   *admission.Gate
	run    string
	lease  string
	ticket string
}

func larkAdmissionRPC(p *extension.Extension, h *Manager) *larkAdmissionLease {
	if h == nil || h.opts.Admission == nil || p.Record.ID != "lark-context" ||
		p.Record.Remote != nil || p.Record.Source != extension.SourceInstalled ||
		!p.Manifest.Runtime.SpeaksRPC() {
		return nil
	}
	return &larkAdmissionLease{gate: h.opts.Admission}
}

func (l *larkAdmissionLease) call(run, method string, payload json.RawMessage) (any, error) {
	if l == nil || run == "" {
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: "extension does not own Lark admission"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch method {
	case "maintenance.lark.begin":
		if !bytes.Equal(bytes.TrimSpace(payload), []byte("{}")) {
			return nil, &rpc.Error{Code: "invalid_params", Message: "Lark admission begin takes no arguments"}
		}
		ticket, err := l.gate.BeginLark(rand.Text())
		if err != nil {
			return nil, &rpc.Error{Code: "maintenance", Message: "Lark admission paused or pending reconciliation"}
		}
		l.run, l.lease, l.ticket = run, rand.Text(), ticket
		return map[string]string{"lease": l.lease}, nil
	case "maintenance.lark.end":
		var lease string
		if err := json.Unmarshal(payload, &lease); err != nil || l.run != run || l.lease == "" || lease != l.lease {
			return nil, &rpc.Error{Code: "maintenance", Message: "no matching Lark admission lease"}
		}
		if err := l.gate.EndLark(l.ticket); err != nil {
			return nil, &rpc.Error{Code: "maintenance", Message: "Lark admission remains unsettled"}
		}
		l.run, l.lease, l.ticket = "", "", ""
	default:
		return nil, &rpc.Error{Code: "unknown_method", Message: "unknown Lark admission operation"}
	}
	return struct{}{}, nil
}
