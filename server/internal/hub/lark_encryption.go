package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/nwverse/persona/server/internal/lark"
)

var larkEncryptionAccount = regexp.MustCompile(`^[0-9]{1,32}$`)

func larkEncryptionBody(w http.ResponseWriter, r *http.Request, value any, limit int64) bool {
	w.Header().Set("Cache-Control", "no-store")
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if dec.Decode(value) != nil || dec.Decode(new(any)) != io.EOF {
		writeErr(w, http.StatusBadRequest, errors.New("加密请求无效，请重试"))
		return false
	}
	return true
}
func writeLarkEncryptionErr(w http.ResponseWriter, err error) {
	var native *lark.NativeOLKError
	code, message, status := "lark_encryption_unavailable", "加密服务暂时不可用，请稍后重试", http.StatusBadGateway
	if errors.As(err, &native) {
		code, message, status = native.Code, native.Message, native.Status
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}
func (h *Hub) larkEncryptionRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/lark/encryption", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		out, err := h.lark.NativeOLKStatus(r.Context())
		if err != nil {
			writeLarkEncryptionErr(w, err)
			return
		}
		writeJSON(w, out)
	})
	api.HandleFunc("POST /api/v1/lark/encryption/unlock", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Account  string `json:"account_id"`
			Password string `json:"password"`
		}
		if !larkEncryptionBody(w, r, &in, 4096) {
			return
		}
		if !larkEncryptionAccount.MatchString(in.Account) || len(in.Password) == 0 || len(utf16.Encode([]rune(in.Password))) > 1024 {
			writeErr(w, 400, errors.New("解锁信息无效"))
			return
		}
		out, err := h.lark.NativeOLKUnlock(r.Context(), in.Account, in.Password)
		if err != nil {
			writeLarkEncryptionErr(w, err)
			return
		}
		writeJSON(w, out)
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/encryption/covers", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Account string `json:"account_id"`
		}
		if !larkEncryptionBody(w, r, &in, 4096) {
			return
		}
		if !larkEncryptionAccount.MatchString(in.Account) || !larkFeedID.MatchString(r.PathValue("id")) {
			writeErr(w, 400, errors.New("会话信息无效"))
			return
		}
		lines, err := h.lark.NativeOLKCovers(r.Context(), in.Account, r.PathValue("id"))
		if err != nil {
			writeLarkEncryptionErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"lines": lines})
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/encryption/prepare", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Account string `json:"account_id"`
			Text    string `json:"text"`
			Cover   string `json:"cover"`
		}
		if !larkEncryptionBody(w, r, &in, 64<<10) {
			return
		}
		if !larkEncryptionAccount.MatchString(in.Account) || !larkFeedID.MatchString(r.PathValue("id")) || strings.TrimSpace(in.Text) == "" || in.Cover == "" || len(utf16.Encode([]rune(in.Text))) > 10000 || len(utf16.Encode([]rune(in.Cover))) > 10000 {
			writeErr(w, 400, errors.New("加密文字或对外展示内容无效"))
			return
		}
		out, err := h.lark.NativeOLKEncrypt(r.Context(), in.Account, r.PathValue("id"), in.Text, in.Cover)
		if err != nil {
			writeLarkEncryptionErr(w, err)
			return
		}
		writeJSON(w, out)
	})
	api.HandleFunc("POST /api/v1/lark/conversations/{id}/encrypted-messages", larkEncryptionNoStore(h.withReplyReceipt("lark_encrypted_send", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Account  string `json:"account_id"`
			Text     string `json:"ciphertext"`
			ClientID string `json:"client_id"`
			Reply    string `json:"reply_id,omitempty"`
		}
		if !larkEncryptionBody(w, r, &in, 64<<10) {
			return
		}
		if !larkEncryptionAccount.MatchString(in.Account) || !larkFeedID.MatchString(r.PathValue("id")) || !lark.ValidClientID(in.ClientID) || !lark.ValidOLKCiphertext(in.Text) || (in.Reply != "" && !larkFeedID.MatchString(in.Reply)) {
			writeErr(w, 400, errors.New("加密消息无效，未发送"))
			return
		}
		id, err := h.lark.SendOLKCiphertext(r.Context(), in.Account, r.PathValue("id"), in.Text, in.ClientID, in.Reply)
		if err != nil {
			writeLarkEncryptionErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "message_id": id})
	})))
}

func larkEncryptionNoStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}
