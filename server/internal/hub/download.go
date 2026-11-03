package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

func (h *Hub) downloadFile(w http.ResponseWriter, r *http.Request, m *Machine) {
	q := r.URL.Query()
	p := proto.FileChunkRequest{Path: q.Get("path"), Cwd: q.Get("cwd")}
	if strings.TrimSpace(p.Path) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	get := func(p proto.FileChunkRequest) (*proto.FileChunk, error) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		var out proto.FileChunk
		if err := m.call(ctx, "file.chunk", p, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	info, err := get(p)
	if err != nil {
		var re *rpc.Error
		if errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method") {
			writeCode(w, http.StatusConflict, proto.CodeNodeOutdated, "这台机器上的 jarvisd 太旧，不能下载完整文件，需要重新部署", nil)
			return
		}
		callErr(w, err)
		return
	}
	if info.Size < 0 || info.Revision == "" {
		writeErr(w, http.StatusBadGateway, errors.New("invalid file metadata"))
		return
	}
	p.Path, p.Revision = info.Path, info.Revision
	w.Header().Set("Content-Type", info.Mime)
	if info.Mime == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("ETag", fmt.Sprintf("%q", info.Revision))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	reader := &remoteFileReader{get: get, request: p, size: info.Size}
	http.ServeContent(w, r, info.Name, info.Modified, reader)
}

type remoteFileReader struct {
	get     func(proto.FileChunkRequest) (*proto.FileChunk, error)
	request proto.FileChunkRequest
	size    int64
	pos     int64
	buf     []byte
}

func (f *remoteFileReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if f.pos >= f.size {
		return 0, io.EOF
	}
	if len(f.buf) == 0 {
		q := f.request
		q.Offset, q.Length = f.pos, int(min(int64(proto.FileDownloadChunkBytes), f.size-f.pos))
		chunk, err := f.get(q)
		if err != nil {
			return 0, err
		}
		if chunk.Offset != q.Offset || chunk.Size != f.size || chunk.Revision != q.Revision || len(chunk.Data) != q.Length {
			return 0, errors.New("file chunk does not match the download")
		}
		f.buf = chunk.Data
	}
	n := copy(p, f.buf)
	f.buf = f.buf[n:]
	f.pos += int64(n)
	return n, nil
}

func (f *remoteFileReader) Seek(offset int64, whence int) (int64, error) {
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = f.pos + offset
	case io.SeekEnd:
		target = f.size + offset
	default:
		return 0, errors.New("invalid seek origin")
	}
	if target < 0 || target > f.size {
		return 0, errors.New("seek outside the file")
	}
	if target != f.pos {
		f.pos, f.buf = target, nil
	}
	return f.pos, nil
}
