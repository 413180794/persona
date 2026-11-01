package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestMailAccountAddArgs(t *testing.T) {
	req, summary, shown, err := mailAccountAdd(json.RawMessage(`{"email":" Me@Example.com ","password":"s3cret","imap_host":"imap.example.com","imap_port":993,"smtp_host":"smtp.example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Email != "me@example.com" || req.Password != "s3cret" || req.Provider != mail.ProviderIMAP || req.Auth != mail.AuthPassword || req.IMAPPort != 993 {
		t.Fatalf("req = %+v", req)
	}
	if summary != "添加邮箱 me@example.com（IMAP imap.example.com:993 · SMTP smtp.example.com）" {
		t.Fatalf("summary = %q", summary)
	}
	if strings.Contains(string(shown), "s3cret") || !strings.Contains(string(shown), "••••••") {
		t.Fatalf("the approval card shows the password: %s", shown)
	}
	if _, s, _, _ := mailAccountAdd(json.RawMessage(`{"email":"a@corp.example","password":"p","provider":"feishu"}`)); !strings.Contains(s, "飞书邮箱") {
		t.Fatalf("feishu summary = %q", s)
	}
	for in, want := range map[string]string{
		`{"email":"a@gmail.com","password":"p"}`: "Google sign-in",
		`{"email":"a@example.org","provider":"gmail"}`: "Google sign-in",
		`{"email":"a@example.com"}`:              "password is required",
		`{"email":"nope"}`:                       "email is required",
	} {
		if _, _, _, err := mailAccountAdd(json.RawMessage(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", in, err, want)
		}
	}
}

// fakeMail records AddAccount; the rest of Mail is not used here.
type fakeMail struct {
	Mail
	added []mail.AddAccountRequest
}

func (f *fakeMail) Tools() []proto.Tool { return nil }

func (f *fakeMail) AddAccount(req mail.AddAccountRequest) (*mail.MailAccount, error) {
	f.added = append(f.added, req)
	return &mail.MailAccount{ID: "acc_1", Email: req.Email, Provider: req.Provider}, nil
}

// The account is added only after the phone approves, from the conversation's own card.
func TestMailAccountAddWaitsForThePhone(t *testing.T) {
	for _, allow := range []bool{false, true} {
		h := newTestHub(t)
		fm := &fakeMail{}
		h.mail = fm
		m := newMachine(h, MachineConfig{ID: "server", Name: "server"})
		done := make(chan string, 1)
		go func() {
			out, err := h.toolMailAccountAdd(context.Background(), m, "s1", json.RawMessage(`{"email":"me@example.com","password":"p"}`))
			if err != nil {
				out = "error: " + err.Error()
			}
			done <- out
		}()
		var ix proto.Interaction
		for deadline := time.Now().Add(5 * time.Second); ; {
			if ps := h.pendingInteractions(); len(ps) == 1 {
				ix = ps[0]
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no approval card")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if ix.SessionID != "s1" || ix.ToolName != "mail_account_add" || !strings.Contains(ix.Summary, "me@example.com") {
			t.Fatalf("card = %+v", ix)
		}
		if len(fm.added) != 0 {
			t.Fatal("added before the phone answered")
		}
		if err := h.resolve(ix.ID, proto.Resolution{Allow: &allow}); err != nil {
			t.Fatal(err)
		}
		out := <-done
		if allow != (len(fm.added) == 1) || (allow && !strings.Contains(out, "Added me@example.com")) || (!allow && !strings.Contains(out, "declined")) {
			t.Fatalf("allow=%v: %q, added %d", allow, out, len(fm.added))
		}
	}
	// Only the assistant gets the tool, and only with mail set up.
	h := newTestHub(t)
	h.mail = &fakeMail{}
	for _, tool := range h.mcpTools(false) {
		if tool.Name == "mail_account_add" {
			t.Fatal("mail_account_add leaked to ordinary sessions")
		}
	}
	found := false
	for _, tool := range h.mcpTools(true) {
		found = found || tool.Name == "mail_account_add"
	}
	if !found || !isAssistantTool("mail_account_add") {
		t.Fatal("the assistant lacks mail_account_add")
	}
}
