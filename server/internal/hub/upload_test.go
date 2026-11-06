package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// uploadNode answers file.upload.* in memory over a real rpc pair, like a node over ssh stdio.
type uploadNode struct {
	mu      sync.Mutex
	old     bool // predates file.upload.*
	calls   []string
	offsets []int64
	data    bytes.Buffer
	aborted bool
	dests   []string // the dest of each file.upload.begin
}

func (f *uploadNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	if f.old {
		return nil, errors.New("unknown method " + method)
	}
	var p struct {
		Name   string `json:"name"`
		Dest   string `json:"dest"`
		Offset int64  `json:"offset"`
		Data   []byte `json:"data"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "file.upload.begin":
		f.dests = append(f.dests, p.Dest)
		return map[string]any{"upload_id": "u1"}, nil
	case "file.upload.chunk":
		f.offsets = append(f.offsets, p.Offset)
		f.data.Write(p.Data)
		return map[string]any{"received": f.data.Len()}, nil
	case "file.upload.end":
		return proto.UploadedFile{Path: "/tmp/report-20261029-153012.pdf", Name: "report-20261029-153012.pdf", Size: int64(f.data.Len())}, nil
	case "file.upload.abort":
		f.aborted = true
		return map[string]any{"ok": true}, nil
	}
	return nil, errors.New("unknown method " + method)
}

func uploadHub(t *testing.T, f *uploadNode) http.Handler {
	t.Helper()
	h := newTestHub(t)
	toNode, nodeIn := io.Pipe()
	toHub, hubIn := io.Pipe()
	hubPeer := rpc.New("h", nodeIn)
	nodePeer := rpc.New("n", hubIn)
	nodePeer.OnRequest = f.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); toNode.Close(); toHub.Close() })
	go hubPeer.Serve(ctx, toHub)
	go nodePeer.Serve(ctx, toNode)
	m := newMachine(h, MachineConfig{ID: "m1", Name: "box"})
	m.peer = hubPeer
	h.byID["m1"] = m
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/machines/{mid}/files", h.withMachine(h.uploadFile))
	return mux
}

func TestUploadStreamsChunks(t *testing.T) {
	f := &uploadNode{}
	mux := uploadHub(t, f)
	data := bytes.Repeat([]byte{7}, proto.UploadChunkBytes*2+12345)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=report.pdf", bytes.NewReader(data)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out proto.UploadedFile
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Path != "/tmp/report-20261029-153012.pdf" || out.Size != int64(len(data)) {
		t.Fatalf("%+v", out)
	}
	want := []int64{0, proto.UploadChunkBytes, 2 * proto.UploadChunkBytes}
	if len(f.offsets) != 3 || f.offsets[0] != want[0] || f.offsets[1] != want[1] || f.offsets[2] != want[2] {
		t.Fatalf("offsets %v", f.offsets)
	}
	if !bytes.Equal(f.data.Bytes(), data) || f.aborted {
		t.Fatal("data or abort")
	}
}

func TestUploadRefused(t *testing.T) {
	// Over the limit: refused before the node hears of it.
	f := &uploadNode{}
	mux := uploadHub(t, f)
	req := httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=big.bin", strings.NewReader("x"))
	req.ContentLength = proto.MaxUploadBytes + 1
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"too_large"`) || len(f.calls) != 0 {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, f.calls)
	}

	// A node from before file uploads: 409 node_outdated, not a bare 502.
	old := &uploadNode{old: true}
	rec = httptest.NewRecorder()
	uploadHub(t, old).ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=a.txt", strings.NewReader("hi")))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"node_outdated"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	// No name.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files", strings.NewReader("hi")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
}

// failingBody gives n bytes, then the connection drops (the app cancelled).
type failingBody struct{ n int }

func (b *failingBody) Read(p []byte) (int, error) {
	if b.n <= 0 {
		return 0, errors.New("connection reset")
	}
	k := min(len(p), b.n)
	b.n -= k
	return k, nil
}

func TestUploadCancelledAborts(t *testing.T) {
	f := &uploadNode{}
	mux := uploadHub(t, f)
	req := httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=a.bin", &failingBody{n: proto.UploadChunkBytes + 10})
	req.ContentLength = 5 * proto.UploadChunkBytes
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == 200 || !f.aborted {
		t.Fatalf("%d aborted=%v calls=%v", rec.Code, f.aborted, f.calls)
	}
	for _, c := range f.calls {
		if c == "file.upload.end" {
			t.Fatal("ended a cut-short upload")
		}
	}
}

func TestUploadForTerminal(t *testing.T) {
	// dest=terminal reaches the node, which keeps the file in its Persona directory.
	f := &uploadNode{}
	mux := uploadHub(t, f)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=image.png&dest=terminal", strings.NewReader("png")))
	if rec.Code != 200 || len(f.dests) != 1 || f.dests[0] != proto.UploadDestTerminal {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, f.dests)
	}
	// dest=health (a report for the health space) reaches the node too.
	hf := &uploadNode{}
	rec = httptest.NewRecorder()
	uploadHub(t, hf).ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=report.pdf&dest=health", strings.NewReader("pdf")))
	if rec.Code != 200 || len(hf.dests) != 1 || hf.dests[0] != proto.UploadDestHealth {
		t.Fatalf("health: %d %s %v", rec.Code, rec.Body, hf.dests)
	}
	// Without dest the node hears none (an old node never has to understand it).
	plain := &uploadNode{}
	rec = httptest.NewRecorder()
	uploadHub(t, plain).ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=a.txt", strings.NewReader("hi")))
	if rec.Code != 200 || len(plain.dests) != 1 || plain.dests[0] != "" {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, plain.dests)
	}

	// The terminal's limit is lower than the chat's, and the answer says what it is.
	req := httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=big.mov&dest=terminal", strings.NewReader("x"))
	req.ContentLength = proto.MaxTerminalUploadBytes + 1
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"too_large"`) || len(f.dests) != 1 {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, f.dests)
	}
	var body struct {
		Limit int64 `json:"limit"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Limit != proto.MaxTerminalUploadBytes {
		t.Fatalf("limit %d", body.Limit)
	}

	// An unknown destination is refused before the node hears of it.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/machines/m1/files?name=a.txt&dest=%2Fetc", strings.NewReader("hi")))
	if rec.Code != http.StatusBadRequest || len(f.dests) != 1 {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, f.dests)
	}
}
