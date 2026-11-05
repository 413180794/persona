package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestKBRPCReadsAndUpdates(t *testing.T) {
	s := kbServiceOn(t, newFakeHub(), newKBStore(), t.TempDir(), nil)
	e := kbEng(t, s)
	c := &kb.Chat{ID: "example-chat", Source: "example", Name: "Example"}
	if err := e.Store.PutChat(c); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, e.Store.Zone())
	if err := e.Store.AppendMessages(c, []kb.Line{{At: at, Sender: "Example", Text: "a searchable line\ncontinued"},
		{At: at.Add(time.Minute), Sender: "Example", Text: "second"}}); err != nil {
		t.Fatal(err)
	}
	ask := func(method, raw string) string {
		t.Helper()
		out, ok, err := s.kbRequest(context.Background(), method, json.RawMessage(raw))
		if !ok || err != nil {
			t.Fatalf("%s: handled=%v err=%v", method, ok, err)
		}
		b, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := ask(assistantwire.MethodKBUpdate, `{"kind":"projects","id":"example","name":"Example","summary":"confirmed context","tags":["work"],"chats":["example-chat"]}`); got != `{"id":"example"}` {
		t.Fatal(got)
	}
	if got := ask(assistantwire.MethodKBList, `{"kind":"projects","tag":"work","limit":1}`); !strings.Contains(got, `"id":"example"`) || !strings.Contains(got, `"tags":["work"]`) {
		t.Fatal(got)
	}
	if got := ask(assistantwire.MethodKBGet, `{"kind":"projects","id":"example"}`); !strings.Contains(got, "confirmed context") || !strings.Contains(got, "example-chat") || strings.Contains(got, `"agents"`) {
		t.Fatal(got)
	}
	if got := ask(assistantwire.MethodKBDay, `{"id":"example-chat","day":"2026-01-02"}`); !strings.Contains(got, `"n":3`) || !strings.Contains(got, `"n":5`) || !strings.Contains(got, `\ncontinued`) {
		t.Fatal(got)
	}
	if got := ask(assistantwire.MethodKBSearch, `{"query":"searchable","messages":true,"limit":1}`); !strings.Contains(got, "messages/2026-01-02.md") {
		t.Fatal(got)
	}
	if got := ask(assistantwire.MethodKBSearch, `{"query":"searchable","messages":false}`); got != `{"hits":[]}` {
		t.Fatal(got)
	}
	if got := ask(assistantwire.MethodKBList, `{"kind":"people"}`); got != `{"items":[],"tags":[],"total":0}` {
		t.Fatal(got)
	}
	if err := e.Store.AppendMessages(c, []kb.Line{{At: time.Now(), Sender: "Example", Text: "recent content"}}); err != nil {
		t.Fatal(err)
	}
	if got := ask(assistantwire.MethodKBGet, `{"kind":"chats","id":"example-chat","recent_chars":0}`); strings.Contains(got, `"recent"`) {
		t.Fatalf("explicit zero must omit recent messages: %s", got)
	}
	if got := ask(assistantwire.MethodKBGet, `{"kind":"chats","id":"example-chat"}`); !strings.Contains(got, "recent content") {
		t.Fatalf("default recent messages: %s", got)
	}
}

func TestKBRPCRejectsInvalidParamsBeforeWriting(t *testing.T) {
	s := kbServiceOn(t, newFakeHub(), newKBStore(), t.TempDir(), nil)
	for _, tc := range []struct{ method, raw string }{
		{assistantwire.MethodKBUpdate, `{"kind":"projects","id":"bad","summmary":"lost correction"}`},
		{assistantwire.MethodKBUpdate, `{"kind":"projects","id":" "}`},
		{assistantwire.MethodKBUpdate, `{"kind":"unknown","id":"bad"}`},
		{assistantwire.MethodKBUpdate, `{"kind":"projects","id":"bad"} {}`},
		{assistantwire.MethodKBUpdate, `null`},
		{assistantwire.MethodKBUpdate, `[]`},
		{assistantwire.MethodKBList, `{"kind":"unknown"}`},
		{assistantwire.MethodKBList, `{"kind":"projects","limit":201}`},
		{assistantwire.MethodKBList, `{"kind":"projects","limit":-1}`},
		{assistantwire.MethodKBList, `{"kind":"projects","offset":-1}`},
		{assistantwire.MethodKBGet, `{"kind":"people"}`},
		{assistantwire.MethodKBGet, `{"kind":"people","id":"x","recent_chars":20001}`},
		{assistantwire.MethodKBGet, `{"kind":"people","id":"x","recent_chars":-1}`},
		{assistantwire.MethodKBDay, `{"id":"example","day":"2026-02-30"}`},
		{assistantwire.MethodKBDay, `{"day":"2026-01-02"}`},
		{assistantwire.MethodKBSearch, `{"query":" "}`},
		{assistantwire.MethodKBSearch, `{"query":"x","limit":201}`},
		{assistantwire.MethodKBSearch, `{"query":"x","limit":-1}`},
		{assistantwire.MethodKBSearch, `{"query":"x","messages":"false"}`},
		{assistantwire.MethodKBUpdate, `{"kind":"projects","id":"bad","summary":"` + strings.Repeat("x", 1<<20) + `"}`},
	} {
		_, handled, err := s.kbRequest(context.Background(), tc.method, json.RawMessage(tc.raw))
		var re *rpc.Error
		if !handled || !errors.As(err, &re) || re.Code != bus.CodeInvalidParams {
			t.Fatalf("%s %s: handled=%v err=%v", tc.method, tc.raw, handled, err)
		}
	}
	if projects := kbEng(t, s).Store.Projects(); len(projects) != 0 {
		t.Fatalf("invalid calls wrote projects: %+v", projects)
	}
	for _, method := range []string{assistantwire.MethodKBGet, assistantwire.MethodKBDay, assistantwire.MethodKBUpdate} {
		raw := `{"kind":"people","id":"missing"}`
		if method == assistantwire.MethodKBDay {
			raw = `{"id":"missing","day":"2026-01-02"}`
		}
		_, _, err := s.kbRequest(context.Background(), method, json.RawMessage(raw))
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != "not_found" {
			t.Fatalf("%s missing: %v", method, err)
		}
	}
	if _, handled, _ := s.kbRPCRequest(context.Background(), "kb.forget", json.RawMessage(`{}`)); handled {
		t.Fatal("public RPC took an internal method")
	}
}

func TestKBRPCListPagesEveryEntity(t *testing.T) {
	s := kbServiceOn(t, newFakeHub(), newKBStore(), t.TempDir(), nil)
	e := kbEng(t, s)
	for i := range 205 {
		id := fmt.Sprintf("example-%03d", i)
		if err := e.Store.PutProject(&kb.Project{Slug: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for offset := 0; ; {
		raw, _ := json.Marshal(assistantwire.KBListParams{Kind: kb.KindProject, Limit: 200, Offset: offset})
		out, handled, err := s.kbRequest(context.Background(), assistantwire.MethodKBList, raw)
		if !handled || err != nil {
			t.Fatalf("offset %d: %v", offset, err)
		}
		page := out.(assistantwire.KBListResult)
		if page.Total != 205 || len(page.Items) != min(200, 205-offset) {
			t.Fatalf("offset %d: %+v", offset, page)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate entity %s", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextOffset == nil {
			break
		}
		if *page.NextOffset != offset+len(page.Items) {
			t.Fatalf("next offset: %d", *page.NextOffset)
		}
		offset = *page.NextOffset
	}
	if len(seen) != 205 {
		t.Fatalf("listed %d of 205 entities", len(seen))
	}
	out, _, err := s.kbRequest(context.Background(), assistantwire.MethodKBList, json.RawMessage(`{"kind":"projects","offset":999999999}`))
	if err != nil {
		t.Fatal(err)
	}
	page := out.(assistantwire.KBListResult)
	if page.Total != 205 || len(page.Items) != 0 || page.NextOffset != nil {
		t.Fatalf("past the end: %+v", page)
	}
}
