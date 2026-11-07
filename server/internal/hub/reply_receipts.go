package hub

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/nwverse/persona/server/internal/compat"
	"io"
	"net/http"
)

// withReplyReceipt gives body-only clients the same durable receipts as client_ops.
// Namespacing keeps arbitrary upstream client IDs separate from explicit operation UUIDs.
func (h *Hub) withReplyReceipt(kind string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.store == nil { // no receipts without the database: the Lark gateway still drops a repeat by its clientId
			next(w, r)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		_ = r.Body.Close()
		if err != nil {
			writeBodyErr(w, err, err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var in struct {
			ClientID string `json:"client_id"`
		}
		if json.Unmarshal(raw, &in) == nil && in.ClientID != "" {
			// Keep the original client_id in the body for the Lark gateway; only the receipt key is derived.
			sum := sha256.Sum256([]byte(compat.ReplyHashPrefix() + kind + ":" + in.ClientID))
			sum[6], sum[8] = sum[6]&0x0f|0x80, sum[8]&0x3f|0x80
			r = r.Clone(r.Context())
			r.Header.Set("X-Persona-Op-Id", fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16]))
		}
		h.withClientOp(kind, next)(w, r)
	}
}
