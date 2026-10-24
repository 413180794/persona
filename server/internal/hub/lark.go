package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// larkRoutes exposes Feishu chats from the user's lark-connector instance to the app's 信息 tab.
func (h *Hub) larkRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/lark/conversations", func(w http.ResponseWriter, r *http.Request) {
		convs, next, err := h.lark.Conversations(r.Context(), r.URL.Query().Get("cursor"), atoi(r.URL.Query().Get("limit")))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs, "next_cursor": next})
	})
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		msgs, older, err := h.lark.Messages(r.Context(), r.PathValue("id"), r.URL.Query().Get("before"), atoi(r.URL.Query().Get("limit")))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"messages": msgs, "has_older": older})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		if err := h.lark.Send(r.Context(), r.PathValue("id"), in.Text); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/read", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Position string `json:"position"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Position == "" {
			writeErr(w, http.StatusBadRequest, errors.New("position is required"))
			return
		}
		if err := h.lark.MarkRead(r.Context(), r.PathValue("id"), in.Position); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	// Images, stickers, avatars, video and files inside messages; the URLs in message parts already point here.
	api.HandleFunc("GET /api/v1/lark/media/{key}", func(w http.ResponseWriter, r *http.Request) {
		h.larkMedia(w, r, "/media/"+url.PathEscape(r.PathValue("key")))
	})
	api.HandleFunc("GET /api/v1/lark/messages/{id}/media/{key}", func(w http.ResponseWriter, r *http.Request) {
		h.larkMedia(w, r, "/messages/"+url.PathEscape(r.PathValue("id"))+"/media/"+url.PathEscape(r.PathValue("key")))
	})
	api.HandleFunc("GET /api/v1/lark/emojis", func(w http.ResponseWriter, r *http.Request) {
		emojis, err := h.lark.Emojis(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeJSON(w, map[string]any{"emojis": emojis})
	})
	api.HandleFunc("POST /api/v1/lark/messages/{id}/card-actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		params, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err := h.lark.CardAction(r.Context(), r.PathValue("id"), r.PathValue("action"), params); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

func (h *Hub) larkMedia(w http.ResponseWriter, r *http.Request, path string) {
	q := url.Values{}
	if size := r.URL.Query().Get("size"); size != "" {
		q.Set("size", size)
	}
	if err := h.lark.Media(r.Context(), w, path, q); err != nil {
		writeErr(w, http.StatusBadGateway, err)
	}
}
