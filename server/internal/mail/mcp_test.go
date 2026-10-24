package mail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPTools(t *testing.T) {
	s := newServiceWithAccount(t, newTestIMAP(t))

	tools := s.Tools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %s schema is not JSON: %v", tool.Name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("tool %s schema type = %v", tool.Name, schema["type"])
		}
	}
	for _, want := range []string{ToolAccounts, ToolSearch, ToolRead, ToolSend} {
		if !names[want] {
			t.Errorf("tool %s is missing", want)
		}
	}
}

func TestMCPAccountsAndSearch(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	srv.appendMessage(t, "INBOX", rawMessage("Quarterly report", "boss@example.com", "Numbers are attached"))

	res := s.Call(ctx, ToolAccounts, json.RawMessage(`{}`), nil)
	if res.IsError {
		t.Fatalf("mail_accounts: %v", res.Content)
	}
	out := resultText(t, res)
	if !strings.Contains(out, "acc_test") || !strings.Contains(out, "me@example.com") {
		t.Errorf("mail_accounts output = %q", out)
	}

	res = s.Call(ctx, ToolSearch, json.RawMessage(`{"account_id":"acc_test","query":"quarterly","limit":5}`), nil)
	if res.IsError {
		t.Fatalf("mail_search: %v", res.Content)
	}
	out = resultText(t, res)
	if !strings.Contains(out, "Quarterly report") || !strings.Contains(out, "boss@example.com") {
		t.Errorf("mail_search output = %q", out)
	}

	res = s.Call(ctx, ToolSearch, json.RawMessage(`{"account_id":"acc_test","query":"no-such-text"}`), nil)
	if res.IsError {
		t.Fatalf("empty search should not be an error: %v", res.Content)
	}
	if !strings.Contains(resultText(t, res), "No messages matched") {
		t.Errorf("empty search output = %q", resultText(t, res))
	}

	res = s.Call(ctx, ToolSearch, json.RawMessage(`not json`), nil)
	if !res.IsError {
		t.Error("malformed arguments should be an error")
	}
}

func TestMCPRead(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	uid := srv.appendMessage(t, "INBOX", rawMessage("Read me", "a@example.com", "the body"))
	args, _ := json.Marshal(map[string]any{"account_id": "acc_test", "uid": uid})
	res := s.Call(ctx, ToolRead, args, nil)
	if res.IsError {
		t.Fatalf("mail_read: %v", res.Content)
	}
	out := resultText(t, res)
	for _, want := range []string{"Subject: Read me", "the body", "a@example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("mail_read output missing %q:\n%s", want, out)
		}
	}

	// Reading does not mark the message as seen.
	msg, err := s.GetMessage(ctx, "acc_test", uint32(uid), "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Seen {
		t.Error("mail_read marked the message as seen")
	}
}

func TestMCPSendNeedsApproval(t *testing.T) {
	s := newServiceWithAccount(t, newTestIMAP(t))
	ctx := context.Background()

	sent := false
	s.sendMail = func(context.Context, *MailAccount, string, []string, []byte) error {
		sent = true
		return nil
	}
	args := json.RawMessage(`{"account_id":"acc_test","to":[{"email":"friend@example.com"}],"subject":"Hi","text":"Hello"}`)

	approved := false
	res := s.Call(ctx, ToolSend, args, func(ctx context.Context, summary string, input json.RawMessage) bool {
		approved = true
		if !strings.Contains(summary, "friend@example.com") || !strings.Contains(summary, "Hi") {
			t.Errorf("approval summary = %q", summary)
		}
		if string(input) != string(args) {
			t.Errorf("approval input = %s, want the original arguments", input)
		}
		return false
	})
	if !approved {
		t.Fatal("approve was not called")
	}
	if !res.IsError {
		t.Error("a declined send must report an error to the model")
	}
	if sent {
		t.Error("mail was sent even though the user declined")
	}

	res = s.Call(ctx, ToolSend, args, func(context.Context, string, json.RawMessage) bool { return true })
	if res.IsError {
		t.Fatalf("approved send: %v", res.Content)
	}
	if !sent {
		t.Error("approved send did not reach the SMTP transport")
	}
}

func TestMCPUnknownTool(t *testing.T) {
	s := newServiceWithAccount(t, newTestIMAP(t))
	res := s.Call(context.Background(), "mail_nope", json.RawMessage(`{}`), nil)
	if !res.IsError || !strings.Contains(resultText(t, res), "unknown tool") {
		t.Errorf("unknown tool result = %+v", res)
	}
}

func TestMCPNoAccounts(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res := s.Call(context.Background(), ToolAccounts, json.RawMessage(`{}`), nil)
	if !res.IsError {
		t.Error("empty account list should be an error so the model asks the user to add one")
	}
}

// resultText flattens a tool result into one string, whatever its content
// shape is.
func resultText(t *testing.T, res interface{ }) string {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var probe struct {
		Content any `json:"content"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	raw, _ := json.Marshal(probe.Content)
	return string(raw)
}
