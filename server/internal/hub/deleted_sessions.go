package hub

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Deleting a conversation (issue #801) only takes it out of the apps' lists: the hub notes when (settings
// sessions.deleted, by "machine/session id") and leaves out of what it sends the apps an offline session that has
// not been used since. The transcript stays on its computer; once the session is used again (resumed, a new
// message) it is listed again. A start that failed (start_error) goes the same way, right away instead of when the
// node forgets it. Running sessions and the assistant's conversations (they archive) cannot be deleted.

const (
	deletedSessionsKey = "sessions.deleted"
	deletedKeepFor     = 30 * 24 * time.Hour // a note older than this is dropped: the node no longer lists the session
)

type deletedBook struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]time.Time // "machine/session id" → when it was deleted
}

// deletedLocked reads the notes once; b.mu is held. A failed read leaves them unloaded (nothing is saved over them).
func (h *Hub) deletedLocked(ctx context.Context) bool {
	b := &h.deleted
	if b.loaded {
		return true
	}
	m := map[string]time.Time{}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, deletedSessionsKey, &m); err != nil {
			h.logf("load deleted sessions: %v", err)
			return false
		}
	}
	if m == nil {
		m = map[string]time.Time{}
	}
	b.m, b.loaded = m, true
	return true
}

// deletedAt is when each deleted session of machine was deleted, by session id.
func (h *Hub) deletedAt(machine string) map[string]time.Time {
	b := &h.deleted
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.deletedLocked(context.Background()) {
		return nil
	}
	var out map[string]time.Time
	prefix := machine + "/"
	for k, t := range b.m {
		if sid, ok := strings.CutPrefix(k, prefix); ok {
			if out == nil {
				out = map[string]time.Time{}
			}
			out[sid] = t
		}
	}
	return out
}

// stillDeleted reports whether s stays out of the list given when it was deleted: it has not run or been used since.
// Finished (done / unknown) counts like offline — a bot's leftover that is not running can go too.
func stillDeleted(s proto.Session, at time.Time) bool {
	finished := s.Status == proto.StatusOffline || s.Status == proto.StatusDone || s.Status == proto.StatusUnknown
	return finished && !s.Starting && !s.Assistant && !s.UpdatedAt.After(at)
}

// shown is list (a machine's decorated sessions) without the ones the user deleted: what the apps get.
func (h *Hub) shown(machine string, list []proto.Session) []proto.Session {
	del := h.deletedAt(machine)
	if len(del) == 0 {
		return list
	}
	out := make([]proto.Session, 0, len(list))
	for _, s := range list {
		if at, ok := del[s.SessionID]; ok && stillDeleted(s, at) {
			continue
		}
		out = append(out, s)
	}
	return out
}

var (
	errDeleteRunning   = errors.New("这个 Agent 还在运行，先关闭再删除")
	errDeleteAssistant = errors.New("助手的对话用归档")
	errDeleteUnknown   = errors.New("没有这个会话")
)

// deleteSession is DELETE /api/v1/sessions/{mid}/{sid}: take an offline session out of the apps' lists.
func (h *Hub) deleteSession(w http.ResponseWriter, r *http.Request, m *Machine) {
	if principalOf(r.Context()).Role != roleFull {
		writeErr(w, http.StatusForbidden, errors.New("read-only role cannot delete conversations"))
		return
	}
	id := r.PathValue("sid")
	var s proto.Session
	found := false
	for _, x := range h.decorated(m) {
		if x.SessionID == id {
			s, found = x, true
			break
		}
	}
	switch {
	case !found:
		writeErr(w, http.StatusNotFound, errDeleteUnknown)
		return
	case s.Assistant:
		writeErr(w, http.StatusBadRequest, errDeleteAssistant)
		return
	case s.Starting || s.Status == proto.StatusWorking || s.Status == proto.StatusBlocked:
		writeErr(w, http.StatusConflict, errDeleteRunning)
		return
	}
	if err := h.noteDeleted(r.Context(), m.cfg.ID, id, time.Now().UTC()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h.sessionsChanged(m) // the apps get the list without it now
	writeJSON(w, map[string]any{"ok": true})
}

// noteDeleted saves that machine's session id was deleted at, dropping notes too old to matter.
func (h *Hub) noteDeleted(ctx context.Context, machine, id string, at time.Time) error {
	if h.store == nil {
		return errors.New("no database")
	}
	b := &h.deleted
	b.mu.Lock()
	defer b.mu.Unlock()
	if !h.deletedLocked(ctx) {
		return errors.New("deleted sessions could not be read")
	}
	next := make(map[string]time.Time, len(b.m)+1)
	for k, t := range b.m {
		if at.Sub(t) < deletedKeepFor {
			next[k] = t
		}
	}
	next[machine+"/"+id] = at
	if err := h.store.PutSetting(ctx, deletedSessionsKey, next); err != nil {
		return err
	}
	b.m = next
	return nil
}
