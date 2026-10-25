package hub

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Pins are the spaces and agents the user keeps at the top of the 开发 list (saved in Postgres, so a reinstall or
// another phone sees the same). Spaces are "machine/space name" — herdr workspace ids change when herdr restarts,
// names do not; agents are "machine/session id".
type Pins struct {
	Spaces   []string `json:"spaces"`
	Sessions []string `json:"sessions"`
}

func (h *Hub) pinRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/pins", func(w http.ResponseWriter, r *http.Request) {
		p := Pins{Spaces: []string{}, Sessions: []string{}}
		if h.store != nil {
			if _, err := h.store.Setting(r.Context(), "pins", &p); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		writeJSON(w, p)
	})
	api.HandleFunc("PUT /api/v1/pins", func(w http.ResponseWriter, r *http.Request) {
		var p Pins
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		if p.Spaces == nil {
			p.Spaces = []string{}
		}
		if p.Sessions == nil {
			p.Sessions = []string{}
		}
		if err := h.store.PutSetting(r.Context(), "pins", p); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, p)
	})
}
