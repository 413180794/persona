package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// larkRoutes exposes Feishu chats from the user's lark-connector instance to the app's 信息 tab.
func (h *Hub) larkRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/lark/conversations", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		convs, next, err := h.lark.Conversations(r.Context(), q.Get("scope"), q.Get("cursor"), atoi(q.Get("limit")))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs, "next_cursor": next})
	})
	api.HandleFunc("GET /api/v1/lark/pinned", func(w http.ResponseWriter, r *http.Request) {
		convs, err := h.lark.Pinned(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs})
	})
	api.HandleFunc("GET /api/v1/lark/labels", func(w http.ResponseWriter, r *http.Request) {
		labels, err := h.lark.Labels(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"labels": labels})
	})
	api.HandleFunc("GET /api/v1/lark/unread-count", func(w http.ResponseWriter, r *http.Request) {
		n, err := h.lark.UnreadTotal(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"count": n})
	})
	api.HandleFunc("GET /api/v1/lark/flags", func(w http.ResponseWriter, r *http.Request) {
		convs, msgs, err := h.lark.Flags(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs, "messages": msgs})
	})
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/labels", func(w http.ResponseWriter, r *http.Request) {
		ids, opts, err := h.lark.ChatLabels(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if ids == nil {
			ids = []string{}
		}
		writeJSON(w, map[string]any{"label_ids": ids, "labels": opts})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		var in lark.Settings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.lark.UpdateSettings(r.Context(), r.PathValue("id"), in); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/unread", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Unread bool `json:"unread"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.lark.SetUnread(r.Context(), r.PathValue("id"), in.Unread); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		page, err := h.lark.Messages(r.Context(), r.PathValue("id"), lark.Window{Before: q.Get("before"), After: q.Get("after"), Around: q.Get("around")},
			atoi(q.Get("limit")), q.Get("topic") == "1")
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, page)
	})
	api.HandleFunc("GET /api/v1/lark/messages/{id}/thread", func(w http.ResponseWriter, r *http.Request) {
		page, err := h.lark.Thread(r.Context(), r.PathValue("id"), r.URL.Query().Get("before"), atoi(r.URL.Query().Get("limit")))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, page)
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
		var in lark.Read
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Position == "" {
			writeErr(w, http.StatusBadRequest, errors.New("position is required"))
			return
		}
		if err := h.lark.MarkRead(r.Context(), r.PathValue("id"), in); err != nil {
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
	// Read-only window onto the rest of lark-connector's agent API (search, docs, calendar, minutes, tasks, contacts…):
	// /api/v1/lark/raw/<path> = /agent/v1/<path>.
	api.HandleFunc("GET /api/v1/lark/raw/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.lark.Raw(r.Context(), w, "GET", rawPath(r), r.URL.Query(), nil, r.Header); err != nil {
			writeErr(w, http.StatusBadGateway, err)
		}
	})
	api.HandleFunc("POST /api/v1/lark/raw/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.lark.Raw(r.Context(), w, "POST", rawPath(r), r.URL.Query(), io.LimitReader(r.Body, 1<<20), r.Header); err != nil {
			writeErr(w, http.StatusBadGateway, err)
		}
	})
	api.HandleFunc("GET /api/v1/lark/emojis", func(w http.ResponseWriter, r *http.Request) {
		emojis, groups, err := h.lark.Emojis(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeJSON(w, map[string]any{"emojis": emojis, "groups": groups})
	})
	api.HandleFunc("GET /api/v1/lark/reactions/quick", func(w http.ResponseWriter, r *http.Request) {
		keys, err := h.lark.QuickReactions(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"keys": keys})
	})
	api.HandleFunc("POST /api/v1/lark/messages/{id}/reactions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Key    string `json:"key"`
			Remove bool   `json:"remove"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Key == "" {
			writeErr(w, http.StatusBadRequest, errors.New("key is required"))
			return
		}
		if err := h.lark.React(r.Context(), r.PathValue("id"), in.Key, in.Remove); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /api/v1/lark/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.lark.Profile(r.Context(), r.PathValue("id"), r.URL.Query().Get("feed_id")))
	})
	api.HandleFunc("POST /api/v1/lark/users/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		feed, err := h.lark.DirectChat(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"feed_id": feed})
	})
	api.HandleFunc("GET /api/v1/lark/messages/{id}/url-preview", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.lark.URLPreview(r.Context(), r.PathValue("id"), r.URL.Query().Get("url")))
	})
	api.HandleFunc("GET /api/v1/lark/moments/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.lark.MomentsPost(r.Context(), r.PathValue("id")))
	})
	api.HandleFunc("GET /api/v1/lark/moments/posts/{id}/comments", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.lark.MomentsComments(r.Context(), r.PathValue("id")))
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

// rawPath is the lark-connector path behind /api/v1/lark/raw, still percent-encoded: ids with "/" (mail threads) must
// reach lark-connector as %2F, not as two path segments.
func rawPath(r *http.Request) string {
	return strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/lark/raw")
}

// larkRaw writes lark-connector's own JSON (already rewritten to hub media URLs) or the error.
func (h *Hub) larkRaw(w http.ResponseWriter) func(json.RawMessage, error) {
	return func(raw json.RawMessage, err error) {
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(raw)
	}
}

func (h *Hub) larkMedia(w http.ResponseWriter, r *http.Request, path string) {
	q := url.Values{}
	if size := r.URL.Query().Get("size"); size != "" {
		q.Set("size", size)
	}
	if err := h.lark.Media(r.Context(), w, path, q, r.Header); err != nil {
		writeErr(w, http.StatusBadGateway, err)
	}
}
