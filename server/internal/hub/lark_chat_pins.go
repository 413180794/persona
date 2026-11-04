package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/nwverse/persona/server/internal/lark"
)

func writeChatPinErr(w http.ResponseWriter, err error) {
	var e *lark.ChatPinError
	if !errors.As(err, &e) {
		writeLarkErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	writeJSON(w, map[string]string{"code": e.Code, "error": e.Message})
}

func (h *Hub) chatPinAccount(w http.ResponseWriter, r *http.Request, expected string) (string, bool) {
	account, err := h.lark.SelectedUserID(r.Context())
	if err != nil {
		writeLarkErr(w, err)
		return "", false
	}
	if expected != "" && expected != account {
		writeChatPinErr(w, &lark.ChatPinError{Status: 409, Code: "lark_account_changed", Message: "飞书账号已切换，请重新打开会话"})
		return "", false
	}
	return account, true
}

func (h *Hub) larkChatPinRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/pins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		account, ok := h.chatPinAccount(w, r, r.URL.Query().Get("account_id"))
		if !ok {
			return
		}
		out, err := h.lark.ChatPins(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), atoi(r.URL.Query().Get("limit")))
		if err != nil {
			writeChatPinErr(w, err)
			return
		}
		if _, ok = h.chatPinAccount(w, r, account); !ok {
			return
		}
		writeJSON(w, map[string]any{"account_id": account, "items": out.Items, "next_cursor": out.NextCursor, "total": out.Total})
	})
	api.HandleFunc("GET /api/v1/lark/conversations/{id}/pin-messages/{message}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		account, ok := h.chatPinAccount(w, r, r.URL.Query().Get("account_id"))
		if !ok {
			return
		}
		target, err := h.lark.ResolveChatPin(r.Context(), r.PathValue("id"), r.PathValue("message"), r.URL.Query().Get("topic") == "1")
		if err != nil {
			writeChatPinErr(w, err)
			return
		}
		if _, ok = h.chatPinAccount(w, r, account); !ok {
			return
		}
		writeJSON(w, map[string]any{"account_id": account, "target": target})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/pins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			Account string `json:"account_id"`
			Message string `json:"message_id"`
			Pin     string `json:"pin_id"`
			Remove  bool   `json:"remove"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || in.Account == "" || !larkFeedID.MatchString(in.Message) || !larkFeedID.MatchString(r.PathValue("id")) || (in.Remove && !larkFeedID.MatchString(in.Pin)) || (!in.Remove && in.Pin != "") {
			writeErr(w, 400, errors.New("Pin 信息无效，请重新打开会话"))
			return
		}
		account, ok := h.chatPinAccount(w, r, in.Account)
		if !ok {
			return
		}
		if err := h.lark.ChangeChatPin(r.Context(), account, r.PathValue("id"), in.Message, in.Pin, in.Remove); err != nil {
			writeChatPinErr(w, err)
			return
		}
		if _, ok = h.chatPinAccount(w, r, account); !ok {
			return
		}
		writeJSON(w, map[string]any{"account_id": account, "pinned": !in.Remove})
	})
}
