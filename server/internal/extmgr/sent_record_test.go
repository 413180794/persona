package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/rpc"
)

// sentExtensionSpec is the spec of an installed extension whose manifest has these connectors (a JSON list), as the
// hub answers its program's calls (host.Spec.Handle).
func sentExtensionSpec(t *testing.T, h *Manager, id, connectors string) host.Spec {
	t.Helper()
	if h.extensions == nil {
		h.extensions = newExtensions(t.TempDir(), nil, t.Logf)
		h.extensions.h = h
	}
	raw := fmt.Sprintf(`{"manifest_version":1,"id":%q,"name":"Example","version":"1.0.0",
		"engines":{"extension_protocol":1},"runtime":{"command":{%q:"bin/unused"}},"activation_events":["on_startup"],
		"contributes":{"connectors":%s}}`, id, runtime.GOOS+"-"+runtime.GOARCH, connectors)
	m, err := extension.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.extensions.reg.AddBuiltin(m, t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
	spec, err := h.extensions.specOf(context.Background(), h.extensions.reg.Get(id), &hubExtension{id: id})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func sentRecord(spec host.Spec, params string) error {
	_, err := spec.Handle(context.Background(), connector.MethodSentRecord, json.RawMessage(params))
	return err
}

func wantRPCCode(t *testing.T, what string, err error, code string) {
	t.Helper()
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != code {
		t.Errorf("%s: want %s, got %v", what, code, err)
	}
}

// An extension's connector that declares sends_as_user can put a send of its own in the ledger. The row says what the
// hub knows (the extension, auto) whatever the params claim; the target keeps short values only; the text is cut and
// cleaned like a gated write's.
func TestSentRecordWritesARow(t *testing.T) {
	h := newTestManager(t)
	st := newSentLedgerStore()
	h.store = st
	spec := sentExtensionSpec(t, h, "example-chat", `[
		{"id":"recordchat","name":"Record Chat","capabilities":["status"],"sends_as_user":true},
		{"id":"recordquiet","name":"Record Quiet","capabilities":["status"]}]`)

	long := strings.Repeat("好", connector.SentTextMost+5)
	params, _ := json.Marshal(map[string]any{
		"contribution": "recordchat",
		"target":       map[string]any{"chat": "chat_1", "reply_to": 3, "to": []string{"a@example.com", "b@example.com"}, "nested": map[string]any{"x": 1}, "long": strings.Repeat("x", 500), "": "no key"},
		"text":         long + "\x00",
		"summary":      "Auto-reply to Example A\nsecond line",
		"session_id":   "conv_1",
		"machine_id":   "box",
		"message_ref":  "msg_9",
		"setting":      "auto_reply",
		// What the hub says itself, however the program claims otherwise.
		"origin":      map[string]any{"extension": "someone-else"},
		"let_through": map[string]any{"kind": "card", "interaction_id": "i1"},
		"action":      "example_send",
		"connector":   "github",
	})
	if err := sentRecord(spec, string(params)); err != nil {
		t.Fatal(err)
	}
	rows := st.all()
	if len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	row := rows[0]
	if row.Connector != "recordchat" || row.Action != "sent.record" || row.Summary != "Auto-reply to Example A" || row.MessageRef != "msg_9" || row.At.IsZero() {
		t.Errorf("row %+v", row)
	}
	if n := len([]rune(row.Text)); n != connector.SentTextMost || strings.Contains(row.Text, "\x00") || row.Text != string([]rune(long)[:connector.SentTextMost]) {
		t.Errorf("text: %d characters", n)
	}
	if o := objectOf(t, row.Origin); o["extension"] != "example-chat" || o["machine_id"] != "box" || o["session_id"] != "conv_1" || len(o) != 3 {
		t.Errorf("origin %s", row.Origin)
	}
	if lt := objectOf(t, row.LetThrough); lt["kind"] != "auto" || lt["setting"] != "auto_reply" || len(lt) != 2 {
		t.Errorf("let through %s", row.LetThrough)
	}
	target := objectOf(t, row.Target)
	if target["chat"] != "chat_1" || target["reply_to"] != float64(3) || fmt.Sprint(target["to"]) != "[a@example.com b@example.com]" ||
		len([]rune(fmt.Sprint(target["long"]))) != 200 || len(target) != 4 {
		t.Errorf("target %s", row.Target)
	}

	// Without a summary or the optional fields: the summary is made from the connector and the target, the origin is
	// the extension alone and the let-through has no setting.
	if err := sentRecord(spec, `{"contribution":"recordchat","target":{"chat":"chat_2"},"text":""}`); err != nil {
		t.Fatal(err)
	}
	row = st.all()[1]
	if row.Summary != "recordchat: chat chat_2" || row.MessageRef != "" || string(row.Origin) != `{"extension":"example-chat"}` ||
		string(row.LetThrough) != `{"kind":"auto"}` {
		t.Errorf("second row %+v", row)
	}
	if err := sentRecord(spec, `{"contribution":"recordchat","target":{},"text":"x"}`); err != nil {
		t.Fatal(err)
	}
	if row = st.all()[2]; row.Summary != "recordchat" || string(row.Target) != `{}` {
		t.Errorf("a row with no target fields: %+v", row)
	}

	// The ledger's read route gives it back like any other row.
	code, out := call(t, h.routes(), "GET", "/api/v1/connectors/sent?connector=recordchat&limit=1", h.cfg.Token, "")
	items, _ := out["items"].([]any)
	if code != 200 || len(items) != 1 || items[0].(map[string]any)["origin"].(map[string]any)["extension"] != "example-chat" {
		t.Errorf("read: %d %v", code, out)
	}
}

// Only a connector of the caller's own that declares sends_as_user may record; anything else is permission_denied and
// writes nothing. Params that are not an object of where it went are invalid_params, also nothing written.
func TestSentRecordRefusals(t *testing.T) {
	h := newTestManager(t)
	st := newSentLedgerStore()
	h.store = st
	spec := sentExtensionSpec(t, h, "example-chat", `[
		{"id":"refusechat","name":"Refuse Chat","capabilities":["status"],"sends_as_user":true},
		{"id":"refusequiet","name":"Refuse Quiet","capabilities":["status"]}]`)
	other := sentExtensionSpec(t, h, "example-other", `[{"id":"refuseother","name":"Refuse Other","capabilities":["status"],"sends_as_user":true}]`)
	plain := sentExtensionSpec(t, h, "example-plain", `[{"id":"refuseplain","name":"Refuse Plain","capabilities":["status"]}]`)

	for _, c := range []struct {
		what string
		spec host.Spec
		id   string
	}{
		{"a connector that does not declare it", spec, "refusequiet"},
		{"another extension's connector, which declares it", spec, "refuseother"},
		{"the same connector from the other extension", other, "refusechat"},
		{"an extension none of whose connectors declare it", plain, "refuseplain"},
		{"a connector that does not exist", spec, "nobody"},
		{"a built-in connector", spec, "github"},
		{"no contribution named", spec, ""},
	} {
		wantRPCCode(t, c.what, sentRecord(c.spec, fmt.Sprintf(`{"contribution":%q,"target":{"chat":"c"},"text":"x"}`, c.id)), extension.CodePermissionDenied)
	}
	wantRPCCode(t, "no params", sentRecord(spec, ``), extension.CodePermissionDenied)
	if rows := st.all(); len(rows) != 0 {
		t.Fatalf("refused, yet written: %+v", rows)
	}

	for what, params := range map[string]string{
		"params that are not an object": `[1]`,
		"no target":                     `{"contribution":"refusechat","text":"x"}`,
		"a null target":                 `{"contribution":"refusechat","target":null,"text":"x"}`,
		"a target that is a string":     `{"contribution":"refusechat","target":"chat_1","text":"x"}`,
		"a target that is a list":       `{"contribution":"refusechat","target":["chat_1"],"text":"x"}`,
		"a text that is a number":       `{"contribution":"refusechat","target":{},"text":5}`,
		"no text":                       `{"contribution":"refusechat","target":{"chat":"c"}}`,
		"a null text":                   `{"contribution":"refusechat","target":{"chat":"c"},"text":null}`,
	} {
		wantRPCCode(t, what, sentRecord(spec, params), "invalid_params")
	}
	if rows := st.all(); len(rows) != 0 {
		t.Fatalf("invalid, yet written: %+v", rows)
	}

	// Switching the extension off ends it at once: the registry is read at every call.
	if err := sentRecord(spec, `{"contribution":"refusechat","target":{},"text":"x"}`); err != nil {
		t.Fatal(err)
	}
	if err := h.extensions.reg.SetEnabled(context.Background(), "example-chat", false, "test"); err != nil {
		t.Fatal(err)
	}
	wantRPCCode(t, "a disabled extension", sentRecord(spec, `{"contribution":"refusechat","target":{},"text":"x"}`), extension.CodePermissionDenied)
	if rows := st.all(); len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
}

// Without a database there is no ledger: the caller is told, nothing breaks.
func TestSentRecordWithoutADatabase(t *testing.T) {
	h := newTestManager(t)
	h.store = newKeepStore()
	spec := sentExtensionSpec(t, h, "example-chat", `[{"id":"nodbchat","name":"No Db Chat","capabilities":["status"],"sends_as_user":true}]`)
	wantRPCCode(t, "no database", sentRecord(spec, `{"contribution":"nodbchat","target":{},"text":"x"}`), "no_database")
}

// What the hub keeps of the params: 20 keys of the target in key order, each name cut to 64 characters, the summary's
// first line cut to 300, the fields the extension names itself to 200, the text to 4000; text must be there (it may be
// empty). A message_ref is kept whole or not at all: over 200 characters it is empty, not cut.
func TestSentRecordLimits(t *testing.T) {
	h := newTestManager(t)
	st := newSentLedgerStore()
	h.store = st
	spec := sentExtensionSpec(t, h, "example-chat", `[{"id":"limitchat","name":"Limit Chat","capabilities":["status"],"sends_as_user":true}]`)
	record := func(params map[string]any) db.SentAsUser {
		t.Helper()
		params["contribution"] = "limitchat"
		if _, ok := params["text"]; !ok {
			params["text"] = ""
		}
		b, _ := json.Marshal(params)
		n := len(st.all())
		if err := sentRecord(spec, string(b)); err != nil {
			t.Fatal(err)
		}
		rows := st.all()
		if len(rows) != n+1 {
			t.Fatalf("rows %d, was %d", len(rows), n)
		}
		return rows[n]
	}
	runes := func(s string) int { return len([]rune(s)) }

	many := map[string]any{}
	for i := 1; i <= 25; i++ {
		many[fmt.Sprintf("k%02d", i)] = "v"
	}
	target := objectOf(t, record(map[string]any{"target": many}).Target)
	if _, ok := target["k20"]; !ok || len(target) != 20 {
		t.Errorf("21 or more keys: %d kept, %v", len(target), target)
	}
	if _, ok := target["k21"]; ok {
		t.Errorf("a key past the 20th was kept: %v", target)
	}
	target = objectOf(t, record(map[string]any{"target": map[string]any{strings.Repeat("好", 70): "v"}}).Target)
	for k := range target {
		if runes(k) != 64 || len(target) != 1 {
			t.Errorf("a key of 70 characters: %d characters, %v", runes(k), target)
		}
	}

	row := record(map[string]any{"target": map[string]any{}, "summary": strings.Repeat("好", 400)})
	if runes(row.Summary) != 300 {
		t.Errorf("a summary of 400 characters: %d", runes(row.Summary))
	}
	row = record(map[string]any{"target": map[string]any{}, "summary": "first line\nsecond line"})
	if row.Summary != "first line" {
		t.Errorf("a summary of two lines: %q", row.Summary)
	}
	// Without a summary it is made from the connector and the target, whose values may have several lines.
	row = record(map[string]any{"target": map[string]any{"chat": "line one\nline two", "thread": "t1"}})
	if row.Summary != "limitchat: chat line one" {
		t.Errorf("a made summary of two lines: %q", row.Summary)
	}

	long := strings.Repeat("好", 300)
	row = record(map[string]any{"target": map[string]any{}, "session_id": long, "machine_id": long, "setting": long, "message_ref": long})
	origin, through := objectOf(t, row.Origin), objectOf(t, row.LetThrough)
	for what, got := range map[string]string{"session_id": fmt.Sprint(origin["session_id"]), "machine_id": fmt.Sprint(origin["machine_id"]),
		"setting": fmt.Sprint(through["setting"])} {
		if runes(got) != 200 {
			t.Errorf("%s of 300 characters: %d kept", what, runes(got))
		}
	}
	if row.MessageRef != "" {
		t.Errorf("a message_ref of 300 characters: %d kept, want none (it is not cut)", runes(row.MessageRef))
	}
	if row = record(map[string]any{"target": map[string]any{}, "message_ref": strings.Repeat("好", 200)}); runes(row.MessageRef) != 200 {
		t.Errorf("a message_ref of 200 characters: %d kept", runes(row.MessageRef))
	}

	// The text may be empty but must be there.
	if row = record(map[string]any{"target": map[string]any{"chat": "c"}, "text": ""}); row.Text != "" {
		t.Errorf("an empty text: %q", row.Text)
	}
	// A string made of nothing once cleaned is not a value of the target.
	target = objectOf(t, record(map[string]any{"target": map[string]any{"chat": "c", "thread": "\x00"}}).Target)
	if _, ok := target["thread"]; ok || target["chat"] != "c" {
		t.Errorf("a value that is empty once cleaned: %v", target)
	}
}

// The user switching a connector off in Settings › Connectors does not stop its extension from recording a send that
// already happened: the ledger is the better for having it.
func TestSentRecordWhileTheConnectorIsSwitchedOff(t *testing.T) {
	h := newTestManager(t)
	st := newSentLedgerStore()
	h.store = st
	h.connectors = h.newConnectors(nil)
	if err := h.addConnector(newThreadsConnector()); err != nil { // its id is "example"
		t.Fatal(err)
	}
	spec := sentExtensionSpec(t, h, "example-chat", `[{"id":"example","name":"Example","capabilities":["status"],"sends_as_user":true}]`)
	if err := h.connectors.SetEnabled("example", false); err != nil || h.connectors.Enabled("example") {
		t.Fatalf("switching it off: %v", err)
	}
	if err := sentRecord(spec, `{"contribution":"example","target":{"chat":"u1"},"text":"sent a moment ago"}`); err != nil {
		t.Fatal(err)
	}
	if rows := st.all(); len(rows) != 1 || rows[0].Connector != "example" || rows[0].Text != "sent a moment ago" {
		t.Errorf("rows %+v", rows)
	}
}

// At most 60 calls at once per extension and then one a second, whether or not a call was allowed; the answer is
// rate_limited with retry_after in seconds, and another extension has its own.
func TestSentRecordIsRateLimited(t *testing.T) {
	h := newTestManager(t)
	st := newSentLedgerStore()
	h.store = st
	spec := sentExtensionSpec(t, h, "example-chat", `[{"id":"ratechat","name":"Rate Chat","capabilities":["status"],"sends_as_user":true},
		{"id":"ratequiet","name":"Rate Quiet","capabilities":["status"]}]`)
	other := sentExtensionSpec(t, h, "example-other", `[{"id":"rateother","name":"Rate Other","capabilities":["status"],"sends_as_user":true}]`)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	h.extensions.sentLimit.now = func() time.Time { return now }
	send := func(s host.Spec, conn string) error {
		return sentRecord(s, fmt.Sprintf(`{"contribution":%q,"target":{"chat":"c"},"text":"x"}`, conn))
	}
	retryAfter := func(err error) float64 {
		t.Helper()
		var re *rpc.Error
		var data struct {
			RetryAfter float64 `json:"retry_after"`
		}
		if !errors.As(err, &re) || re.Code != "rate_limited" || json.Unmarshal(re.Data, &data) != nil || data.RetryAfter <= 0 {
			t.Fatalf("want rate_limited with retry_after, got %v", err)
		}
		return data.RetryAfter
	}

	for i := range 60 {
		if err := send(spec, "ratechat"); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if after := retryAfter(send(spec, "ratechat")); after > 1 {
		t.Errorf("one token is a second away, retry_after %v", after)
	}
	if rows := st.all(); len(rows) != 60 {
		t.Fatalf("%d rows", len(rows))
	}
	if err := send(other, "rateother"); err != nil {
		t.Errorf("another extension has its own: %v", err)
	}
	now = now.Add(400 * time.Millisecond)
	if after := retryAfter(send(spec, "ratechat")); after > 0.7 {
		t.Errorf("0.4 s later, retry_after %v", after)
	}
	now = now.Add(700 * time.Millisecond) // 1.1 s since the 60th: one token, less what the refused call before took (none)
	if err := send(spec, "ratechat"); err != nil {
		t.Errorf("a second later: %v", err)
	}
	retryAfter(send(spec, "ratechat"))
	// Refilling stops at 60: an hour later it is 60 calls again, not 3600.
	now = now.Add(time.Hour)
	for i := range 60 {
		if err := send(spec, "ratechat"); err != nil {
			t.Fatalf("call %d after an hour: %v", i+1, err)
		}
	}
	retryAfter(send(spec, "ratechat"))

	// A call that is refused counts too: the limit is the first thing checked.
	now = now.Add(time.Hour)
	for range 60 {
		wantRPCCode(t, "an undeclared connector", send(spec, "ratequiet"), extension.CodePermissionDenied)
	}
	retryAfter(send(spec, "ratechat"))
}

// failingSentStore is the ledger when the database is down.
type failingSentStore struct{ *sentLedgerStore }

func (failingSentStore) RecordSentAsUser(context.Context, db.SentAsUser) (int64, error) {
	return 0, errors.New("connection reset by peer, text was: secret words")
}

// A ledger that cannot be written is an error with a message and no code, which does not say what was sent.
func TestSentRecordWhenTheLedgerCannotBeWritten(t *testing.T) {
	h := newTestManager(t)
	h.store = failingSentStore{newSentLedgerStore()}
	spec := sentExtensionSpec(t, h, "example-chat", `[{"id":"downchat","name":"Down Chat","capabilities":["status"],"sends_as_user":true}]`)
	err := sentRecord(spec, `{"contribution":"downchat","target":{"chat":"c"},"text":"secret words"}`)
	var re *rpc.Error
	if err == nil || errors.As(err, &re) || strings.Contains(err.Error(), "secret words") || !strings.Contains(err.Error(), "could not be written") {
		t.Errorf("want a plain error that does not repeat what the database said, got %v", err)
	}
}
