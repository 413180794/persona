package hub

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/rpc"
)

// rewindNode is a node that answers session.rewind with answer/err and keeps what it was asked.
type rewindNode struct {
	mu     sync.Mutex
	params []string
	answer any
	err    error
	hub    *Hub
}

func newRewindNode(t *testing.T) *rewindNode {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	rn := &rewindNode{answer: map[string]any{"prompt": "x", "restore": "conversation", "code_restored": false, "input_cleared": true}}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		rn.mu.Lock()
		defer rn.mu.Unlock()
		if method != "session.rewind" {
			return map[string]any{}, nil
		}
		rn.params = append(rn.params, string(params))
		return rn.answer, rn.err
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	h := &Hub{byID: map[string]*Machine{}, logger: log.New(io.Discard, "", 0)}
	m := newMachine(h, MachineConfig{ID: "m1", Name: "m1"})
	m.peer = hubSide
	h.byID["m1"] = m
	rn.hub = h
	return rn
}

func (rn *rewindNode) post(role, sid, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/sessions/m1/"+sid+"/rewind", strings.NewReader(body))
	r.SetPathValue("mid", "m1")
	r.SetPathValue("sid", sid)
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: role, ID: "dev"}))
	w := httptest.NewRecorder()
	rn.hub.withMachine(rn.hub.rewindSession)(w, r)
	return w
}

const rewindBody = `{"prompt":"fix the flaky test","occurrence":2,"prompts_after":3,"restore":"code_and_conversation"}`

// Rewinding restores files and drops the conversation: a full device only, refused before the node hears of it.
func TestRewindNeedsAFullDevice(t *testing.T) {
	rn := newRewindNode(t)
	for _, role := range []string{roleAnswer, roleExtension} {
		if w := rn.post(role, "s1", rewindBody); w.Code != http.StatusForbidden {
			t.Errorf("%s was let through: %d", role, w.Code)
		}
		if allowed(principal{ID: "dev", Role: role}, http.MethodPost, "/api/v1/sessions/m1/s1/rewind") && role == roleAnswer {
			t.Errorf("an answer-only device may POST the route")
		}
	}
	if allowed(principal{ID: "dev_link", Role: roleLink}, http.MethodPost, "/api/v1/sessions/m1/s1/rewind") {
		t.Error("a link may POST the route")
	}
	if len(rn.params) != 0 {
		t.Fatalf("the node heard of a refused request: %q", rn.params)
	}
	data, err := os.ReadFile("testdata/route_scopes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "-                    POST /api/v1/sessions/{mid}/{sid}/rewind") {
		t.Error("the route is not full-credentials-only in the extension scopes")
	}
}

// Only the named fields reach the node; the answer comes back as the node gave it.
func TestRewindPassesOnlyItsFields(t *testing.T) {
	rn := newRewindNode(t)
	w := rn.post(roleFull, "s1", `{"prompt":"fix the flaky test","occurrence":2,"prompts_after":3,"restore":"code_and_conversation","keys":["enter"],"cwd":"/etc","session_id":"other"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"input_cleared":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(rn.params[0]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got["session_id"] != "s1" || got["prompt"] != "fix the flaky test" || got["occurrence"] != float64(2) || got["prompts_after"] != float64(3) || got["restore"] != "code_and_conversation" {
		t.Fatalf("the node was sent %v", got)
	}
	// prompts_after is optional.
	rn.post(roleFull, "s1", `{"prompt":"x","occurrence":1,"restore":"conversation"}`)
	if strings.Contains(rn.params[1], "prompts_after") {
		t.Errorf("an absent prompts_after was sent: %s", rn.params[1])
	}
}

func TestRewindRejectsBadRequestsBeforeTheNode(t *testing.T) {
	rn := newRewindNode(t)
	for name, body := range map[string]string{
		"no body":          ``,
		"not json":         `rewind`,
		"no prompt":        `{"occurrence":1,"restore":"conversation"}`,
		"blank prompt":     `{"prompt":"  ","occurrence":1,"restore":"conversation"}`,
		"no occurrence":    `{"prompt":"x","restore":"conversation"}`,
		"huge occurrence":  `{"prompt":"x","occurrence":1000000,"restore":"conversation"}`,
		"negative after":   `{"prompt":"x","occurrence":1,"prompts_after":-1,"restore":"conversation"}`,
		"no restore":       `{"prompt":"x","occurrence":1}`,
		"summarize":        `{"prompt":"x","occurrence":1,"restore":"summarize"}`,
		"code only":        `{"prompt":"x","occurrence":1,"restore":"code"}`,
		"oversized prompt": `{"prompt":"` + strings.Repeat("a", rewindMaxPrompt+1) + `","occurrence":1,"restore":"conversation"}`,
	} {
		if w := rn.post(roleFull, "s1", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := rn.post(roleFull, "pane:w1:p2", rewindBody); w.Code != http.StatusBadRequest {
		t.Errorf("a pane address: %d", w.Code)
	}
	if len(rn.params) != 0 {
		t.Fatalf("a refused request reached the node: %q", rn.params)
	}
}

// The node's refusals come back as 409 with their code and message; an older node is node_outdated.
func TestRewindRefusalsAndOldNodes(t *testing.T) {
	rn := newRewindNode(t)
	for _, code := range []string{"dialog_open", "input_draft", "rewind_busy", "rewind_unsupported", "rewind_nothing", "rewind_mismatch", "rewind_failed"} {
		rn.mu.Lock()
		rn.err = &rpc.Error{Code: code, Message: "说明 " + code}
		rn.mu.Unlock()
		w := rn.post(roleFull, "s1", rewindBody)
		var body struct{ Code, Error string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusConflict || body.Code != code || body.Error != "说明 "+code {
			t.Errorf("%s: %d %s", code, w.Code, w.Body.String())
		}
	}
	rn.mu.Lock()
	rn.err = &rpc.Error{Message: "unknown method session.rewind"}
	rn.mu.Unlock()
	w := rn.post(roleFull, "s1", rewindBody)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"node_outdated"`) {
		t.Fatalf("old node: %d %s", w.Code, w.Body.String())
	}
}
