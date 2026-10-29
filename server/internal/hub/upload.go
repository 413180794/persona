package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// uploadFile streams a file from the phone to a machine's temp directory (docs/protocol.md「发送文件」):
// POST /api/v1/machines/{mid}/files?name=<file name>, the raw bytes as the body. The body is read a chunk at a time
// and each chunk is one file.upload.chunk call, so neither side holds the file and no JSON line carries all of it.
// The app cancelling the request (or going away) aborts the upload and the node removes what it wrote.
func (h *Hub) uploadFile(w http.ResponseWriter, r *http.Request, m *Machine) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	size := r.ContentLength
	switch {
	case name == "":
		writeErr(w, http.StatusBadRequest, errors.New("name is required"))
		return
	case size < 0:
		writeErr(w, http.StatusLengthRequired, errors.New("Content-Length is required"))
		return
	case size > proto.MaxUploadBytes:
		writeCode(w, http.StatusRequestEntityTooLarge, proto.CodeTooLarge,
			fmt.Sprintf("文件超过 %d MB", proto.MaxUploadBytes>>20), map[string]any{"limit": proto.MaxUploadBytes})
		return
	}
	ctx := r.Context()
	step := func(method string, params, out any) error {
		c, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return m.call(c, method, params, out)
	}
	var begin struct {
		UploadID string `json:"upload_id"`
	}
	if err := step("file.upload.begin", map[string]any{"name": name, "size": size}, &begin); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == "" && strings.HasPrefix(re.Message, "unknown method") {
			writeCode(w, http.StatusConflict, proto.CodeNodeOutdated, m.cfg.Name+" 上的 jarvisd 太旧，不能接收文件，需要重新部署", nil)
			return
		}
		callErr(w, err)
		return
	}
	done := false
	defer func() {
		if !done { // not ctx: it is cancelled when the app gives up, which is when this matters most
			c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = m.call(c, "file.upload.abort", map[string]any{"upload_id": begin.UploadID}, nil)
		}
	}()
	body := http.MaxBytesReader(w, r.Body, size)
	buf := make([]byte, proto.UploadChunkBytes)
	var off int64
	for {
		n, err := io.ReadFull(body, buf)
		if n > 0 {
			if cerr := step("file.upload.chunk", map[string]any{"upload_id": begin.UploadID, "offset": off, "data": buf[:n]}, nil); cerr != nil {
				callErr(w, cerr)
				return
			}
			off += int64(n)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	if off != size {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("upload cut short: %d of %d bytes", off, size))
		return
	}
	var out proto.UploadedFile
	if err := step("file.upload.end", map[string]any{"upload_id": begin.UploadID}, &out); err != nil {
		callErr(w, err)
		return
	}
	done = true
	h.logf("[%s] file from the phone: %d bytes", m.cfg.ID, out.Size)
	writeJSON(w, out)
}

// writeCode answers with an error code the app acts on, plus extra fields.
func writeCode(w http.ResponseWriter, status int, code, msg string, extra map[string]any) {
	body := map[string]any{"error": msg, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
