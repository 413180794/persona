package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/nwverse/persona/server/internal/lark"
)

func (h *Hub) larkDisplayRoutes(api larkMux) {
	api.HandleFunc("GET /api/v1/lark/message-display", func(w http.ResponseWriter, r *http.Request) {
		settings, err := h.larkOf(r).MessageDisplay(r.Context(), nil)
		if err != nil {
			writeLarkDisplayErr(w, err)
			return
		}
		writeJSON(w, settings)
	})
	api.HandleFunc("POST /api/v1/lark/message-display", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AccountID string `json:"account_id"`
			Key       string `json:"key"`
			Value     *bool  `json:"value"`
			Expected  *bool  `json:"expected"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil || in.AccountID == "" || in.Value == nil || in.Expected == nil || (in.Key != "anti_recall" && in.Key != "show_edit_history") {
			writeErr(w, http.StatusBadRequest, errors.New("invalid message display settings"))
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			writeErr(w, http.StatusBadRequest, errors.New("invalid request body"))
			return
		}
		settings, err := h.larkOf(r).MessageDisplay(r.Context(), &lark.MessageDisplayChange{AccountID: in.AccountID, Key: in.Key, Value: *in.Value, Expected: *in.Expected})
		if err != nil {
			writeLarkDisplayErr(w, err)
			return
		}
		h.broadcast(map[string]any{"type": "lark", "feed_ids": []string{}, "settings_changed": true})
		writeJSON(w, settings)
	})
}

func writeLarkDisplayErr(w http.ResponseWriter, err error) {
	if errors.Is(err, lark.ErrNotLoggedIn) {
		writeLarkErr(w, err)
		return
	}
	writeErr(w, lark.MessageDisplayErrorStatus(err), err)
}
