package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

func (h *Hub) lockTestRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/devices/{id}/lock-tests", h.serveLockTest)
	api.HandleFunc("GET /api/v1/devices/{id}/lock-tests/{test_id}", h.serveLockTest)
	api.HandleFunc("POST /api/v1/devices/{id}/lock-tests/{test_id}/receipt", h.serveLockTest)
}

func (h *Hub) serveLockTest(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r.Context())
	deviceID, testID := r.PathValue("id"), r.PathValue("test_id")
	// The device ID is the credential ID, never the untrusted client_hello device_id.
	if p.ID == "" || p.PairHash != "" || p.ID != deviceID {
		writeErr(w, http.StatusForbidden, errors.New("a matching device credential is required"))
		return
	}
	if h.store == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("lock test storage is unavailable"))
		return
	}
	if r.Method == http.MethodGet {
		test, err := h.store.LockTest(r.Context(), deviceID, testID)
		h.writeLockTest(w, test, err, http.StatusNotFound)
		return
	}
	var input struct {
		DelaySeconds      *int  `json:"delay_seconds"`
		NotificationShown *bool `json:"notification_shown"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid lock test request"))
		return
	}
	if testID != "" {
		test, err := h.store.ReceiveLockTest(r.Context(), deviceID, testID, input.NotificationShown, time.Now())
		h.writeLockTest(w, test, err, http.StatusConflict)
		return
	}
	delay := 30
	if input.DelaySeconds != nil {
		delay = *input.DelaySeconds
	}
	if delay < 1 || delay > 3600 {
		writeErr(w, http.StatusBadRequest, errors.New("delay_seconds must be between 1 and 3600"))
		return
	}
	now := time.Now()
	test := db.LockTest{TestID: "lt_" + randomHex(16), DeviceID: deviceID,
		CreatedAt: now, DueAt: now.Add(time.Duration(delay) * time.Second), Status: "scheduled"}
	if err := h.store.CreateLockTest(r.Context(), test); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, test)
}

func (h *Hub) writeLockTest(w http.ResponseWriter, test *db.LockTest, err error, missingStatus int) {
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
	} else if test == nil {
		writeErr(w, missingStatus, errors.New("lock test not found or not sent"))
	} else {
		writeJSON(w, test)
	}
}

func (h *Hub) lockTestClient(deviceID string) *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *client
	for c := range h.clients {
		if c.device != deviceID || c.deviceSessionID == 0 || !c.has("lock_test") {
			continue
		}
		if best == nil || c.since.After(best.since) {
			best = c
		}
	}
	return best
}

func (h *Hub) dispatchLockTests(ctx context.Context, now time.Time) error {
	tests, err := h.store.DueLockTests(ctx, now)
	if err != nil {
		return err
	}
	for _, test := range tests {
		c := h.lockTestClient(test.DeviceID)
		var sessionID int64
		if c != nil {
			sessionID = c.deviceSessionID
		}
		claimed, err := h.store.AttemptLockTest(ctx, test.TestID, sessionID, now)
		if err != nil {
			return err
		}
		if !claimed || c == nil {
			continue
		}
		// Namespace every new payload key to avoid iOS Envelope decoding collisions.
		c.sendJSON(map[string]any{
			"type": "lock_test", "lock_test_id": test.TestID,
			"lock_test_due_at": test.DueAt, "lock_test_sent_at": now,
			"lock_test_title": i18n.T(c.lang(), "hub-locktest.title"), "lock_test_body": i18n.T(c.lang(), "hub-locktest.body"),
		})
	}
	return nil
}

func (h *Hub) startLockTests(ctx context.Context) (func(), error) {
	if h.store == nil {
		return func() {}, nil
	}
	if err := h.store.RecoverLockTests(ctx, time.Now()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				callCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				if err := h.dispatchLockTests(callCtx, now); err != nil && ctx.Err() == nil {
					h.logf("dispatch lock tests: %v", err)
				}
				stop()
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
