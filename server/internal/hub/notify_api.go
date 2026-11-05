package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/sdk/go/wire"
)

// POST /api/v1/notify lets an extension with the notify scope (or any full credential) alert the user's devices, at the level it
// decides: what deserves a nudge (an agent stuck, a scheduled task failing, the same session not twice in half an
// hour) is the extension's call, the delivery is the hub's (docs/extensions.md「发通知」, #1477).

// Notification levels a caller may ask for (UNNotificationInterruptionLevel). "critical" needs an Apple entitlement and
// is not offered; "passive" never alerts, which is not what this is for.
const (
	levelActive        = "active"
	levelTimeSensitive = "time-sensitive"
)

// notifyRequest is the body of POST /api/v1/notify.
type notifyRequest struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	MachineID string `json:"machine_id,omitempty"` // with session_id: a tap opens that session; muted sessions get nothing
	SessionID string `json:"session_id,omitempty"`
	Level     string `json:"level,omitempty"` // active (default) | time-sensitive
	// ID: the same id again replaces the earlier alert on the phone (apns-collapse-id) instead of stacking.
	ID string `json:"id,omitempty"`

	// Extension notification: only accepted when the caller is an extension token (roleExtension, with the notify scope).
	UI      string              `json:"ui,omitempty"`
	State   string              `json:"state,omitempty"`
	Actions []wire.NotifyAction `json:"actions,omitempty"`
}

const (
	notifyTitleMost = 120
	notifyBodyMost  = 1000
	// notifyIDMost leaves room for notifyCollapse's prefix inside APNs' 64-byte apns-collapse-id.
	notifyIDMost  = 56
	notifyRefMost = 128 // machine_id, session_id
	notifyUIMost  = 128 // extension ui contribution id
	// notifyStateMost bounds state payload passed to ui.action.
	notifyStateMost                  = 2000
	notifyActionsMax                 = 3
	notifyActionIDMost               = 128
	notifyActionTitleMost            = 40
	notifyActionInputPlaceholderMost = 80
	// notifyPayloadMost keeps the whole push under APNs' 4 KB with room for hub_id; the relay shortens alerts
	// itself (sealFor), a direct push would be refused.
	notifyPayloadMost = 3500
	// notifyCollapse keeps a caller's ids apart from the hub's own (interaction ids).
	notifyCollapse = "notify:"
	// kindAlert is the WebSocket notify event's kind for these (docs/protocol.md「常驻连接通知」).
	kindAlert = "alert"
)

func (r *notifyRequest) validate() error {
	r.Title, r.Body = strings.TrimSpace(r.Title), strings.TrimSpace(r.Body)
	switch {
	case r.Title == "" && r.Body == "":
		return errors.New("title or body is required")
	case utf8.RuneCountInString(r.Title) > notifyTitleMost:
		return errors.New("title is too long")
	case utf8.RuneCountInString(r.Body) > notifyBodyMost:
		return errors.New("body is too long")
	case len(r.ID) > notifyIDMost:
		return errors.New("id is too long")
	case len(r.MachineID) > notifyRefMost || len(r.SessionID) > notifyRefMost:
		return errors.New("machine_id or session_id is too long")
	case (r.MachineID == "") != (r.SessionID == ""):
		return errors.New("machine_id and session_id go together")
	}
	switch r.Level {
	case "":
		r.Level = levelActive
	case levelActive, levelTimeSensitive:
	default:
		return errors.New("level must be active or time-sensitive")
	}

	r.UI = strings.TrimSpace(r.UI)
	if len(r.UI) > notifyUIMost {
		return errors.New("ui is too long")
	}
	if len(r.State) > notifyStateMost {
		return errors.New("state is too long")
	}
	if len(r.Actions) > notifyActionsMax {
		return errors.New("actions cannot have more than 3 buttons")
	}
	seen := make(map[string]bool, len(r.Actions))
	for i := range r.Actions {
		a := &r.Actions[i]
		a.ID, a.Title = strings.TrimSpace(a.ID), strings.TrimSpace(a.Title)
		if a.ID == "" {
			return errors.New("action id is required")
		}
		if len(a.ID) > notifyActionIDMost {
			return errors.New("action id is too long")
		}
		if seen[a.ID] {
			return errors.New("action ids must be unique")
		}
		seen[a.ID] = true
		if a.Title == "" {
			return errors.New("action title is required")
		}
		if utf8.RuneCountInString(a.Title) > notifyActionTitleMost {
			return errors.New("action title is too long")
		}
		switch a.Role {
		case "", "normal", "destructive":
		default:
			return errors.New("action role must be normal or destructive")
		}
		if a.Input != nil && utf8.RuneCountInString(a.Input.Placeholder) > notifyActionInputPlaceholderMost {
			return errors.New("action input placeholder is too long")
		}
	}
	return nil
}

// actionIDsHash computes an 8-hex hash of the action IDs for the category identifier (ext.<extension>.<hash>).
func actionIDsHash(actions []wire.NotifyAction) string {
	ids := make([]string, len(actions))
	for i, a := range actions {
		ids[i] = a.ID
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(sum[:4])
}

// notifyPayload is the APNs alert for a caller's notification. Like the interaction alerts it threads by session and
// carries machine_id / session_id, so a tap opens the session. For extension callers (extID != ""), missing session threads
// by extension.<extID>, actions set aps.category (ext.<extID>.<hash>), and extension_notification carries the action spec.
func notifyPayload(r notifyRequest, machine, extID string) map[string]any {
	alert := map[string]any{"title": r.Title, "body": r.Body}
	if machine != "" {
		alert["subtitle"] = machine
	}
	aps := map[string]any{"alert": alert, "sound": "default", "interruption-level": r.Level}
	if r.SessionID != "" {
		aps["thread-id"] = r.SessionID
	} else if extID != "" {
		aps["thread-id"] = "extension." + extID
	}
	if extID != "" && len(r.Actions) > 0 {
		aps["category"] = "ext." + extID + "." + actionIDsHash(r.Actions)
	}
	payload := map[string]any{"aps": aps, "machine_id": r.MachineID, "session_id": r.SessionID}
	if extID != "" {
		extNotif := map[string]any{"extension": extID}
		if r.UI != "" {
			extNotif["ui"] = r.UI
		}
		if r.State != "" {
			extNotif["state"] = r.State
		}
		if len(r.Actions) > 0 {
			extNotif["actions"] = r.Actions
		}
		payload["extension_notification"] = extNotif
	}
	return payload
}

func (h *Hub) notifyRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/notify", func(w http.ResponseWriter, r *http.Request) {
		p := principalOf(r.Context())
		var in notifyRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if p.Role != roleExtension && (len(in.Actions) > 0 || in.UI != "" || in.State != "") {
			writeErr(w, http.StatusBadRequest, errors.New("actions, ui and state are only accepted for extension callers"))
			return
		}
		if err := in.validate(); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		machine := ""
		if in.SessionID != "" {
			if h.muted(in.MachineID, in.SessionID) {
				writeJSON(w, map[string]any{"sent": false, "reason": "muted"})
				return
			}
			machine = h.machineName(in.MachineID)
		}
		extID := ""
		if p.Role == roleExtension {
			extID = p.Extension
		}
		payload := notifyPayload(in, machine, extID)
		if raw, _ := json.Marshal(payload); len(raw) > notifyPayloadMost {
			writeErr(w, http.StatusBadRequest, errors.New("notification is too large"))
			return
		}
		collapse := ""
		if in.ID != "" {
			collapse = notifyCollapse + in.ID
		}
		// Something that needs you: devices that turned off 需要你 (prefs.input) get none of these either.
		wants := pushWants{Kind: "input"}.fn()
		h.push.send(payload, collapse, wants)
		h.notifyWS(kindAlert, payload, wants)
		writeJSON(w, map[string]any{"sent": true})
	})
}
