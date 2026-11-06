package hub

import (
	"errors"
	"net/http"
	"strings"

	"rsc.io/qr"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The devices page: the credentials the hub has handed out, revoking one, and a QR code to pair a phone.

func (h *Hub) adminDeviceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/devices", func(w http.ResponseWriter, r *http.Request) {
		if h.memberRefuses(w) {
			return
		}
		list := []db.DeviceToken{}
		if h.store != nil {
			var err error
			if list, err = h.store.DeviceTokens(r.Context()); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		writeJSON(w, map[string]any{"devices": list})
	})
	mux.HandleFunc("DELETE /api/v1/admin/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		if h.memberRefuses(w) {
			return
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		id := r.PathValue("id")
		ok, err := h.store.RevokeDeviceToken(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such device"))
			return
		}
		h.revoked(id)
		h.sendRevocations(newlyReachable(nil, h.meshView()))
		h.logf("admin: device token %s revoked", id)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/admin/devices/pair", func(w http.ResponseWriter, r *http.Request) {
		if h.memberRefuses(w) {
			return
		}
		p, err := h.adminPairCode(r, "")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		rows, size, err := qrRows(p.PairURL)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.logf("admin: pairing code for a phone")
		writeJSON(w, map[string]any{"pair_url": p.PairURL, "url": p.URL, "expires_at": p.ExpiresAt, "fp": p.FP,
			"qr": map[string]any{"size": size, "rows": rows}})
	})
}

// qrRows is text's QR code (level L, like `personad hub pair`), one string of 0 / 1 per row, without the quiet zone:
// the page draws it with its own colors.
func qrRows(text string) ([]string, int, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]string, code.Size)
	var b strings.Builder
	for y := range code.Size {
		b.Reset()
		for x := range code.Size {
			if code.Black(x, y) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows[y] = b.String()
	}
	return rows, code.Size, nil
}

// memberRefuses answers 409 member_hub for what the center manages for the whole group (devices): a member's admin
// page sends the user to the center's.
func (h *Hub) memberRefuses(w http.ResponseWriter) bool {
	if !h.isMember() {
		return false
	}
	c := h.centerID()
	name := h.hubNameOf(c)
	if name == "" {
		name = shortID(c)
	}
	writeErrCode(w, http.StatusConflict, "member_hub", i18n.Err("hub-admin_devices.member_hub", i18n.Args{"name": name}))
	return true
}
