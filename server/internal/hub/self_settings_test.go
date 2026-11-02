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

type selfSettingsMail struct {
	Mail
	added []mail.AddAccountRequest
}

func (f *selfSettingsMail) Tools() []proto.Tool { return nil }
func (f *selfSettingsMail) AddAccount(req mail.AddAccountRequest) (*mail.MailAccount, error) {
	f.added = append(f.added, req)
	return &mail.MailAccount{ID: "acc_1", Email: req.Email, Provider: req.Provider}, nil
}

func TestSelfSettingsMailRequest(t *testing.T) {
	req, summary, err := mailAccountRequest(json.RawMessage(`{"email":" Me@Example.com ","imap_host":"imap.example.com","imap_port":993,"smtp_host":"smtp.example.com"}`))
	if err != nil || req.Email != "me@example.com" || req.Password != "" || req.Provider != mail.ProviderIMAP || !strings.Contains(summary, "imap.example.com:993") {
		t.Fatalf("request %+v, summary %q, error %v", req, summary, err)
	}
	for _, input := range []string{`{"email":"a@gmail.com"}`, `{"email":"a@example.com","provider":"gmail"}`, `{"email":"nope"}`} {
		if _, _, err := mailAccountRequest(json.RawMessage(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestSelfSettingsMailNeedsPhoneInput(t *testing.T) {
	for _, allow := range []bool{false, true} {
		h := newTestHub(t)
		fm := &selfSettingsMail{}
		h.mail = fm
		m := newMachine(h, MachineConfig{ID: "server", Name: "server"})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := h.toolJarvisSettings(ctx, m, "s1", json.RawMessage(`{"action":"add","key":"mail.accounts","value":{"email":"me@example.com"}}`))
			done <- err
		}()
		var ix proto.Interaction
		for ctx.Err() == nil {
			if pending := h.pendingInteractions(); len(pending) == 1 {
				ix = pending[0]
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if ix.ID == "" || len(ix.Fields) != 1 || !ix.Fields[0].Secret || len(fm.added) != 0 {
			t.Fatal("missing secure approval card")
		}
		if allow {
			if err := h.resolve(ix.ID, proto.Resolution{Allow: &allow}); err == nil || len(h.pendingInteractions()) != 1 {
				t.Fatal("quick approval consumed an incomplete form")
			}
		}
		if err := h.resolve(ix.ID, proto.Resolution{Allow: &allow, Values: map[string]string{"password": "example-password"}}); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("settings operation did not complete")
		}
		if allow != (len(fm.added) == 1) {
			t.Fatal("account was not gated by approval")
		}
		if allow && fm.added[0].Password != "example-password" {
			t.Fatal("phone input was not passed to mail")
		}
	}
}

func TestSelfSettingsRedactsSecretsWithoutChangingDelivery(t *testing.T) {
	r := proto.Resolution{Values: map[string]string{"password": "example-password", "name": "example"}}
	kept := redacted(proto.Interaction{Fields: []proto.Field{{Key: "password", Secret: true}}}, r)
	if kept.Values["password"] != "<redacted>" || kept.Values["name"] != "example" || r.Values["password"] != "example-password" {
		t.Fatal("redaction changed the answer or leaked a secret")
	}
	payload := interactionPayload(proto.Interaction{Kind: proto.IxPermission, Fields: []proto.Field{{Key: "password", Secret: true}}}, "server")
	if payload["aps"].(map[string]any)["category"] != "INTERACTION_INPUT" {
		t.Fatal("notification offers quick approval")
	}
}

func TestSelfSettingsRulesAndNotesPersist(t *testing.T) {
	h, _ := modelTestHub(t)
	ctx := context.Background()
	h.cfg.Inbox.Rules = "fallback"
	change, err := h.textSetting(ctx, json.RawMessage(`"custom"`), "规则", h.inboxRules, "inbox.rules")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = change.apply(ctx, nil); err != nil || h.inboxRules(ctx) != "custom" {
		t.Fatalf("rules: %v", err)
	}
	change, err = h.textSetting(ctx, json.RawMessage(`""`), "规则", h.inboxRules, "inbox.rules")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = change.apply(ctx, nil); err != nil || h.inboxRules(ctx) != "fallback" {
		t.Fatalf("cleared rules: %v", err)
	}
	if err = h.saveModelRecord(ctx, "assistant", 0, ModelBinding{Provider: "launcher.selected", Model: "chosen"}); err != nil {
		t.Fatal(err)
	}
	change, err = h.setAssistantNotes(ctx, json.RawMessage(`"example notes"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = change.apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if h.assistantNotes(ctx) != "example notes" || h.assistantConfigLegacy(ctx).Launcher == "selected" {
		t.Fatal("notes changed legacy model inheritance")
	}
}

func TestSelfSettingsToolOnlyForAssistant(t *testing.T) {
	h := newTestHub(t)
	for _, tool := range h.mcpTools(false) {
		if tool.Name == "jarvis_settings" {
			t.Fatal("ordinary agent gets self settings")
		}
	}
	found := false
	for _, tool := range h.mcpTools(true) {
		found = found || tool.Name == "jarvis_settings"
	}
	if !found || !isAssistantTool("jarvis_settings") {
		t.Fatal("assistant lacks self settings")
	}
	out, err := h.toolJarvisSettings(context.Background(), nil, "", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "inbox.rules") || strings.Contains(out, "- mail.accounts") {
		t.Fatalf("list: %q %v", out, err)
	}
}
