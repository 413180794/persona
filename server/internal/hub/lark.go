package hub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
)

// larkRoutes exposes Feishu chats from the user's lark-connector instance to the app's 信息 tab.
func (h *Hub) larkRoutes(mux *http.ServeMux) {
	api := larkMux{mux, h}
	h.larkAccountRoutes(api)
	h.larkAttachmentRoutes(api)
	h.larkDisplayRoutes(api)
	h.larkEncryptionRoutes(api)
	h.larkChatPinRoutes(api)
	api.HandleFunc("GET /api/v1/lark/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.larkStatus(r.Context()))
	})
	api.HandleFunc("GET /api/v1/lark/conversations", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("account_bound") == "1" {
			h.larkConversationSnapshot(w, r, false)
			return
		}
		convs, next, err := h.larkOf(r).Conversations(r.Context(), q.Get("scope"), q.Get("cursor"), atoi(q.Get("limit")))
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs, "next_cursor": next})
	})
	api.HandleFunc("GET /api/v1/lark/pinned", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("account_bound") == "1" {
			h.larkConversationSnapshot(w, r, true)
			return
		}
		convs, err := h.larkOf(r).Pinned(r.Context())
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs})
	})
	api.HandleFunc("GET /api/v1/lark/labels", func(w http.ResponseWriter, r *http.Request) {
		labels, err := h.larkOf(r).Labels(r.Context())
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"labels": labels})
	})
	api.HandleFunc("GET /api/v1/lark/unread-count", func(w http.ResponseWriter, r *http.Request) {
		n, err := h.larkOf(r).UnreadTotal(r.Context())
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"count": n})
	})
	api.HandleFunc("GET /api/v1/lark/flags", func(w http.ResponseWriter, r *http.Request) {
		convs, msgs, err := h.larkOf(r).Flags(r.Context())
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"conversations": convs, "messages": msgs})
	})
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/labels", func(w http.ResponseWriter, r *http.Request) {
		ids, opts, err := h.larkOf(r).ChatLabels(r.Context(), r.PathValue("id"))
		if err != nil {
			writeLarkErr(w, err)
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
		if err := h.larkOf(r).UpdateSettings(r.Context(), r.PathValue("id"), in); err != nil {
			writeLarkErr(w, err)
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
		if err := h.larkOf(r).SetUnread(r.Context(), r.PathValue("id"), in.Unread); err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// The client may not know the chat is a topic group (opened from a profile, an old inbox item): ChatPage checks,
		// and a page of threads says topic so the client shows them as topics.
		page, err := h.larkOf(r).ChatPage(r.Context(), r.PathValue("id"), lark.Window{Before: q.Get("before"), After: q.Get("after"), Around: q.Get("around")},
			atoi(q.Get("limit")), q.Get("topic") == "1")
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, page)
	})
	api.HandleFunc("GET /api/v1/lark/messages/{id}/thread", func(w http.ResponseWriter, r *http.Request) {
		page, err := h.larkOf(r).Thread(r.Context(), r.PathValue("id"), r.URL.Query().Get("before"), atoi(r.URL.Query().Get("limit")))
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, page)
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/messages", h.withReplyReceipt("lark_send", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string         `json:"text"`
			ClientID string         `json:"client_id"` // the app's idempotency key; a retry with the same one posts once
			Mentions []lark.Mention `json:"mentions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		if err := checkMentions(in.Mentions); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		// A missing or malformed client_id is not an error: Send makes one up, as it did before apps sent it.
		if !lark.ValidClientID(in.ClientID) {
			in.ClientID = ""
		}
		id, err := h.larkOf(r).Send(r.Context(), r.PathValue("id"), in.Text, in.ClientID, in.Mentions...)
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		out := map[string]any{"ok": true}
		if id != "" {
			out["message_id"] = id
		}
		writeJSON(w, out)
	}))
	// Replies to one message: quoted in its chat, or with thread=true inside its thread (a topic group's topic).
	// Both the integration branch's always-thread route and master's thread flag live here as one registration.
	api.HandleFunc("POST /api/v1/lark/messages/{id}/replies", h.withReplyReceipt("lark_thread_reply", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string         `json:"text"`
			Thread   bool           `json:"thread"`
			ClientID string         `json:"client_id"`
			Mentions []lark.Mention `json:"mentions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		if err := checkMentions(in.Mentions); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if !lark.ValidClientID(in.ClientID) {
			in.ClientID = ""
		}
		id, err := h.larkOf(r).Reply(r.Context(), r.PathValue("id"), in.Text, in.Thread, false, in.ClientID, in.Mentions...)
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		out := map[string]any{"ok": true}
		if id != "" {
			out["message_id"] = id
		}
		writeJSON(w, out)
	}))
	// The chat's members for the @ picker: all of them (up to limit), or with q the ones matching it.
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		members, err := h.larkOf(r).Members(r.Context(), r.PathValue("id"), q.Get("q"), atoi(q.Get("limit")))
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"members": members})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/read", func(w http.ResponseWriter, r *http.Request) {
		var in lark.Read
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Position == "" {
			writeErr(w, http.StatusBadRequest, errors.New("position is required"))
			return
		}
		if err := h.larkOf(r).MarkRead(r.Context(), r.PathValue("id"), in); err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	// Read receipts of the user's messages in one chat; messages lark-connector has none for are left out.
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/read-states", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			MessageIDs []string `json:"message_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if len(in.MessageIDs) > lark.MaxReadStates {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("at most %d message_ids", lark.MaxReadStates))
			return
		}
		states, err := h.larkOf(r).ReadStates(r.Context(), r.PathValue("id"), in.MessageIDs)
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"states": states})
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
		if err := h.larkOf(r).Raw(r.Context(), w, "GET", rawPath(r), r.URL.Query(), nil, r.Header); err != nil {
			writeLarkErr(w, err)
		}
	})
	api.HandleFunc("POST /api/v1/lark/raw/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.larkOf(r).Raw(r.Context(), w, "POST", rawPath(r), r.URL.Query(), io.LimitReader(r.Body, 1<<20), r.Header); err != nil {
			writeLarkErr(w, err)
		}
	})
	api.HandleFunc("GET /api/v1/lark/emojis", func(w http.ResponseWriter, r *http.Request) {
		emojis, groups, err := h.larkOf(r).Emojis(r.Context())
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeJSON(w, map[string]any{"emojis": emojis, "groups": groups})
	})
	api.HandleFunc("GET /api/v1/lark/reactions/quick", func(w http.ResponseWriter, r *http.Request) {
		keys, err := h.larkOf(r).QuickReactions(r.Context())
		if err != nil {
			writeLarkErr(w, err)
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
		if err := h.larkOf(r).React(r.Context(), r.PathValue("id"), in.Key, in.Remove); err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /api/v1/lark/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.larkOf(r).Profile(r.Context(), r.PathValue("id"), r.URL.Query().Get("feed_id")))
	})
	api.HandleFunc("POST /api/v1/lark/users/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		feed, err := h.larkOf(r).DirectChat(r.Context(), r.PathValue("id"))
		if err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"feed_id": feed})
	})
	api.HandleFunc("GET /api/v1/lark/messages/{id}/url-preview", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.larkOf(r).URLPreview(r.Context(), r.PathValue("id"), r.URL.Query().Get("url")))
	})
	api.HandleFunc("GET /api/v1/lark/moments/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.larkOf(r).MomentsPost(r.Context(), r.PathValue("id")))
	})
	api.HandleFunc("GET /api/v1/lark/moments/posts/{id}/comments", func(w http.ResponseWriter, r *http.Request) {
		h.larkRaw(w)(h.larkOf(r).MomentsComments(r.Context(), r.PathValue("id")))
	})
	api.HandleFunc("POST /api/v1/lark/messages/{id}/card-actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		params, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err := h.larkOf(r).CardAction(r.Context(), r.PathValue("id"), r.PathValue("action"), params); err != nil {
			writeLarkErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

// larkInboxClientID derives one send ID per persisted inbox item, including AI drafts.
// It survives retries and restarts without depending on mutable item fields.
func larkInboxClientID(itemID string) string {
	b := sha256.Sum256([]byte("jarvis:inbox_reply:" + itemID))
	b[6], b[8] = b[6]&0x0f|0x80, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// checkMentions rejects what lark-connector would: more than lark.MaxMentions, or one without a user id or name. The text
// is not checked for the literal "@name"; lark-connector decides what a mention without it means.
func checkMentions(ms []lark.Mention) error {
	if len(ms) > lark.MaxMentions {
		return fmt.Errorf("at most %d mentions", lark.MaxMentions)
	}
	for _, m := range ms {
		if strings.TrimSpace(m.UserID) == "" || strings.TrimSpace(m.Name) == "" {
			return errors.New("a mention needs user_id and name")
		}
	}
	return nil
}

// larkNotLoggedIn is the error code of a 502 caused by lark-connector having lost its Feishu session, so the app can
// say "log in to Feishu in lark-connector again" instead of a generic failure.
const larkNotLoggedIn = "lark_not_logged_in"

// writeLarkErr answers a failed lark-connector call with 502; a lark.ErrNotLoggedIn also carries code larkNotLoggedIn.
func writeLarkErr(w http.ResponseWriter, err error) {
	if !errors.Is(err, lark.ErrNotLoggedIn) {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "code": larkNotLoggedIn})
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
			writeLarkErr(w, err)
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
	if err := h.larkOf(r).Media(r.Context(), w, path, q, r.Header); err != nil {
		writeLarkErr(w, err)
	}
}

// LarkStatus is 设置 › 连接器 › 飞书: whether the hub has an lark-connector to talk to, who is signed in there, and when its
// realtime events last came. Configured is hub.json's lark (a container to read the token from, or a token).
type LarkStatus struct {
	Configured  bool   `json:"configured"`
	Connected   bool   `json:"connected"`
	Name        string `json:"name,omitempty"`
	Error       string `json:"error,omitempty"`
	LastEventAt int64  `json:"last_event_at,omitempty"`
}

func (h *Hub) larkStatus(ctx context.Context) LarkStatus {
	st := LarkStatus{Configured: h.cfg.Lark.Container != "" || h.cfg.Lark.Token != "", LastEventAt: h.larkLastEvent.Load()}
	if !st.Configured || h.lark == nil {
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	me, err := h.lark.Me(ctx) // asked each time: the page is where one checks the connection works now
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Connected, st.Name = true, me.Name
	return st
}
