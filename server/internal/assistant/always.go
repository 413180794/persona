package assistant

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
)

type alwaysGrantStore interface {
	AlwaysGrants(context.Context, string) ([]db.AlwaysGrant, error)
	RevokeAlways(context.Context, string, string) error
}

func (s *Service) alwaysRoutes() {
	serve := func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("full access required"))
			return
		}
		id := r.PathValue("id")
		if !validSessionID(id) {
			apierr.Write(w, http.StatusBadRequest, errors.New("invalid conversation id"))
			return
		}
		st, ok := s.store().(alwaysGrantStore)
		if !ok {
			apierr.Write(w, http.StatusServiceUnavailable, errors.New("permission store unavailable"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), historyTimeout)
		defer cancel()
		if r.Method == http.MethodGet {
			grants, err := st.AlwaysGrants(ctx, id)
			if err != nil {
				apierr.Write(w, http.StatusInternalServerError, err)
				return
			}
			if grants == nil {
				grants = []db.AlwaysGrant{}
			}
			apierr.WriteJSON(w, map[string]any{"grants": grants})
			return
		}
		hash := r.PathValue("hash")
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != hash {
			apierr.Write(w, http.StatusBadRequest, errors.New("invalid grant id"))
			return
		}
		if err := st.RevokeAlways(ctx, id, hash); err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
		// Delete first: a reset failure must never restore the persisted permission. Also reset on
		// retries, since a prior request may have committed the deletion and lost its node connection.
		resetCtx, resetCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer resetCancel()
		machine := s.settings(resetCtx).MachineID
		if s.devs != nil {
			for _, m := range s.devs.List() {
				if sess, exists := s.devs.Session(m.ID, id); exists && sess.Assistant {
					machine = m.ID
					break
				}
			}
		}
		var reset struct {
			OK              bool `json:"ok"`
			RestartRequired bool `json:"restart_required"`
		}
		err = s.call(resetCtx, machine, "assistant.permissions.reset", map[string]any{"session_id": id}, &reset)
		apierr.WriteJSON(w, map[string]any{"ok": true, "restart_required": err != nil || !reset.OK || reset.RestartRequired})
	}
	s.handle("GET /api/v1/assistant/conversations/{id}/always", serve)
	s.handle("DELETE /api/v1/assistant/conversations/{id}/always/{hash}", serve)
}
