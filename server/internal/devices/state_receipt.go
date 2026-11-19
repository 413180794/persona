package devices

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/rpc"
)

type nodeStateReceiptStore interface {
	NodeSessionReceipt(context.Context, string, string) (string, error)
	PutNodeSessionReceipt(context.Context, string, string, string, string, db.NodeSession) error
}

var stateRevision = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (m *Machine) stateReceipt(ctx context.Context, method string, params json.RawMessage) (any, error) {
	st, ok := m.d.opts.Store.(nodeStateReceiptStore)
	if !ok {
		return nil, errors.New("node state receipt store unavailable")
	}
	var p struct {
		SessionID string          `json:"session_id"`
		Revision  string          `json:"revision"`
		Previous  string          `json:"previous"`
		State     *db.NodeSession `json:"state"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.SessionID == "" || len(p.SessionID) > 256 {
		return nil, errors.New("invalid session for node state receipt")
	}
	if method == nodeproto.MethodStateReceipt {
		revision, err := st.NodeSessionReceipt(ctx, m.cfg.ID, p.SessionID)
		return nodeproto.StateRevision{Revision: revision}, err
	}
	if p.State == nil || !stateRevision.MatchString(p.Revision) ||
		p.Previous != "" && !stateRevision.MatchString(p.Previous) || p.Previous == p.Revision {
		return nil, errors.New("invalid node state receipt revision")
	}
	done, err := m.d.opts.Admission.Admit("node-state-receipt")
	if err != nil {
		return nil, &rpc.Error{Code: nodeproto.CodeStatePaused, Message: "retry the retained node state after maintenance"}
	}
	defer done()
	sctx, cancel := m.storeCtx()
	defer cancel()
	err = st.PutNodeSessionReceipt(sctx, m.cfg.ID, p.SessionID, p.Previous, p.Revision, *p.State)
	if errors.Is(err, db.ErrNodeSessionRevision) {
		return nil, &rpc.Error{Code: nodeproto.CodeStatePredecessor, Message: err.Error()}
	}
	return nodeproto.StateRevision{Revision: p.Revision}, err
}
