package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/screen"
)

const dialogScreen = "────────────────────\n Do you want to proceed?\n ❯ 1. Yes\n   2. Type something.\n   3. No\n"

func TestScreenAnswer(t *testing.T) {
	d := screen.ParseDialog(dialogScreen)
	p, err := screenAnswer("s1", d, proto.Resolution{Option: "3"})
	if err != nil || p["rev"] != d.Rev || p["option"] != "3" || !slices.Equal(p["keys"].([]string), []string{"3"}) {
		t.Fatalf("%v %v", p, err)
	}
	p, _ = screenAnswer("s1", d, proto.Resolution{Option: "2", Text: "use the staging db"})
	if !slices.Equal(p["pre_keys"].([]string), []string{"2"}) || !slices.Equal(p["keys"].([]string), []string{"enter"}) || p["text"] != "use the staging db" {
		t.Fatalf("type something: %v", p)
	}
	// An older app sends keys: they stand, still checked against the card's revision.
	p, _ = screenAnswer("s1", d, proto.Resolution{Keys: []string{"1"}})
	if p["rev"] != d.Rev || p["option"] != nil || !slices.Equal(p["keys"].([]string), []string{"1"}) {
		t.Fatalf("legacy: %v", p)
	}
	if _, err := screenAnswer("s1", d, proto.Resolution{Option: "9"}); err == nil {
		t.Fatal("no such option")
	}
	// No dialog read: the terminal card's keys go as they are.
	if p, _ := screenAnswer("s1", nil, proto.Resolution{Keys: []string{"esc"}}); p["rev"] != nil {
		t.Fatalf("%v", p)
	}
}

func TestResolveScreenChanged(t *testing.T) {
	h := newTestHub(t)
	changed := &rpc.Error{Code: screen.PromptChanged, Message: "变了", Data: json.RawMessage(`{"delivered":"none"}`)}
	h.interactions["scr_a"] = &pending{ix: proto.Interaction{ID: "scr_a", Kind: proto.IxScreen},
		local: func(proto.Resolution) error { return changed }}
	err := h.resolve("scr_a", proto.Resolution{Option: "1"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.PromptChanged {
		t.Fatalf("got %v", err)
	}
	if len(h.pendingInteractions()) != 0 {
		t.Fatal("the old card stays gone (a new one follows)")
	}
	if err := h.resolve("scr_a", proto.Resolution{}); !errors.Is(err, errNotPending) {
		t.Fatalf("second answer: %v", err)
	}

	// Over HTTP: 409 with the code, 410 once gone.
	h.interactions["scr_b"] = &pending{ix: proto.Interaction{ID: "scr_b", Kind: proto.IxScreen},
		local: func(proto.Resolution) error { return changed }}
	mux := h.routes()
	for _, want := range []int{http.StatusConflict, http.StatusGone} {
		req := httptest.NewRequest("POST", "/api/v1/interactions/scr_b/resolve", strings.NewReader(`{"option":"1"}`))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body)
		}
		if want == http.StatusConflict && !strings.Contains(w.Body.String(), `"code":"prompt_changed"`) {
			t.Fatal(w.Body.String())
		}
	}
}
