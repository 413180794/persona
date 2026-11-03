package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

type downloadNode struct {
	mu       sync.Mutex
	data     []byte
	requests []proto.FileChunkRequest
	old      bool
	bad      bool
}

func (f *downloadNode) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.old {
		return nil, errors.New("unknown method " + method)
	}
	if method != "file.chunk" {
		return nil, errors.New("unexpected method")
	}
	var p proto.FileChunkRequest
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	f.requests = append(f.requests, p)
	out := proto.FileChunk{Path: "/work/preview.mp4", Name: "preview.mp4", Size: int64(len(f.data)), Mime: "video/mp4", Revision: "r1", Modified: time.Unix(1700000000, 0), Offset: p.Offset}
	if p.Length > 0 {
		out.Data = f.data[p.Offset:min(int64(len(f.data)), p.Offset+int64(p.Length))]
		if f.bad {
			out.Revision = "r2"
		}
	}
	return out, nil
}

func downloadHub(t *testing.T, f *downloadNode) http.Handler {
	t.Helper()
	h := newTestHub(t)
	toNode, nodeIn := io.Pipe()
	toHub, hubIn := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", nodeIn), rpc.New("n", hubIn)
	nodePeer.OnRequest = f.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); toNode.Close(); toHub.Close() })
	go hubPeer.Serve(ctx, toHub)
	go nodePeer.Serve(ctx, toNode)
	m := newMachine(h, MachineConfig{ID: "m1", Name: "box"})
	m.peer = hubPeer
	h.byID["m1"] = m
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/machines/{mid}/file/download", h.withMachine(h.downloadFile))
	return mux
}

func TestDownloadStreamsWholeFileAndRanges(t *testing.T) {
	data := bytes.Repeat([]byte{0, 255, 2, 3}, proto.FileDownloadChunkBytes+9)
	f := &downloadNode{data: data}
	mux := downloadHub(t, f)
	for _, test := range []struct {
		rangeHeader  string
		code         int
		want         []byte
		contentRange string
	}{
		{"", 200, data, ""}, {"bytes=0-1", 206, data[:2], "bytes 0-1/" + strconv.Itoa(len(data))},
		{"bytes=-17", 206, data[len(data)-17:], "bytes " + strconv.Itoa(len(data)-17) + "-" + strconv.Itoa(len(data)-1) + "/" + strconv.Itoa(len(data))},
		{"bytes=1048570-1048590", 206, data[1048570:1048591], "bytes 1048570-1048590/" + strconv.Itoa(len(data))},
	} {
		req := httptest.NewRequest("GET", "/api/v1/machines/m1/file/download?path=preview.mp4&cwd=/work", nil)
		if test.rangeHeader != "" {
			req.Header.Set("Range", test.rangeHeader)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != test.code || !bytes.Equal(rec.Body.Bytes(), test.want) || rec.Header().Get("Content-Range") != test.contentRange {
			t.Fatalf("range %q: status=%d size=%d headers=%v", test.rangeHeader, rec.Code, rec.Body.Len(), rec.Header())
		}
		if rec.Header().Get("Accept-Ranges") != "bytes" || rec.Header().Get("Content-Type") != "video/mp4" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal(rec.Header())
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.requests {
		if p.Length > proto.FileDownloadChunkBytes {
			t.Fatal("unbounded read")
		}
		if p.Length > 0 && p.Revision != "r1" {
			t.Fatal("no revision")
		}
	}
}

func TestDownloadRefusesOldNodesAndBadChunks(t *testing.T) {
	for _, test := range []struct {
		f           *downloadNode
		path        string
		rangeHeader string
		code        int
	}{
		{&downloadNode{old: true}, "?path=x.mp4", "", 409}, {&downloadNode{}, "", "", 400},
		{&downloadNode{data: []byte{0, 1}}, "?path=x.mp4", "bytes=8-10", 416},
	} {
		req := httptest.NewRequest("GET", "/api/v1/machines/m1/file/download"+test.path, nil)
		req.Header.Set("Range", test.rangeHeader)
		rec := httptest.NewRecorder()
		downloadHub(t, test.f).ServeHTTP(rec, req)
		if rec.Code != test.code {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	f := &downloadNode{data: []byte{0, 1, 2}, bad: true}
	rec := httptest.NewRecorder()
	downloadHub(t, f).ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/machines/m1/file/download?path=x.mp4", nil))
	if rec.Body.Len() != 0 {
		t.Fatal("changed chunk was sent")
	}
	if !strings.Contains(rec.Header().Get("Content-Length"), "3") {
		t.Fatal(rec.Header())
	}
}

func TestDownloadReaderCancellationAndSeek(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &remoteFileReader{size: 10, get: func(proto.FileChunkRequest) (*proto.FileChunk, error) { return nil, ctx.Err() }}
	if _, err := reader.Read(make([]byte, 3)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := reader.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek")
	}
	if _, err := reader.Seek(11, io.SeekStart); err == nil {
		t.Fatal("seek after EOF")
	}
	if _, err := reader.Seek(0, 99); err == nil {
		t.Fatal("invalid whence")
	}
}
