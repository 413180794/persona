package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/mail"
)

func TestForeignScript(t *testing.T) {
	cases := []struct {
		text, target string
		want         bool
	}{
		{"Can we move the design review to Tuesday afternoon? I'm travelling.", "zh-Hans", true},
		{"周四我临时要出差，分享能不能挪到周二下午？", "zh-Hans", false},
		{"关于 fetchAll 和 loadAll 的 naming，我觉得 PR #42 可以合了", "zh-Hans", false}, // Chinese with Latin words
		{"OK", "zh-Hans", false}, // too little to tell
		{"来週の会議はキャンセルになりました。よろしくお願いします。", "zh-Hans", true},
		{"来週の会議はキャンセルになりました。よろしくお願いします。", "ja", false},
		{"下周的会议取消了，请大家相互转告一下这个消息", "ja", true},
		{"Bonjour, la réunion de mardi est annulée pour tout le monde.", "en", false}, // same script
		{"下周的会议取消了，请大家相互转告一下这个消息", "en", true},
	}
	for _, c := range cases {
		if got := foreignScript(c.text, c.target); got != c.want {
			t.Errorf("%q → %s: %v, want %v", c.text, c.target, got, c.want)
		}
	}
}

type fakeInboxMail struct {
	Mail
	msgs map[uint32]*mail.MailMessage
}

func (f fakeInboxMail) GetMessage(ctx context.Context, accountID string, uid uint32, mailbox string) (*mail.MailMessage, error) {
	return f.msgs[uid], nil
}

func TestNewMailIsTranslatedBeforeItIsOpened(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	model, setReply := mutableModel(t, `{"subject":"设计评审能改期吗？","text":"你好，周四我要出差。"}`)
	h.triageAI = model
	h.mail = fakeInboxMail{msgs: map[uint32]*mail.MailMessage{
		7: {MailSummary: mail.MailSummary{Subject: "Can we move the design review?"}, Text: "Hi, I'm travelling on Thursday, so I won't make the review."},
	}}
	// One device asked for Chinese, mail in scope.
	prefs := defaultPrefs()
	prefs.Translate = TranslatePrefs{Enabled: true, Target: "zh-Hans", Mail: true}
	if err := h.push.setPrefs("dev1", prefs); err != nil {
		t.Fatal(err)
	}
	en := &InboxItem{ID: "mail:a:t:7", Source: "mail", Kind: KindMail, Title: "Can we move the design review?",
		Preview: "Hi, I'm travelling on Thursday, so I won't make the review.", Last: "7", BodyOf: "7", UpdatedAt: time.Now(),
		Mail: &InboxMail{AccountID: "a", Mailbox: "INBOX", UIDs: []uint32{7}}}
	zh := &InboxItem{ID: "mail:a:u:8", Source: "mail", Kind: KindMail, Title: "下周的分享能不能提前到周二？",
		Preview: "周四我临时要出差，分享能不能挪到周二下午？", Last: "8", BodyOf: "8", UpdatedAt: time.Now(),
		Mail: &InboxMail{AccountID: "a", Mailbox: "INBOX", UIDs: []uint32{8}}}
	h.inbox.items[en.ID], h.inbox.items[zh.ID] = en, zh

	h.translateInbox(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.inbox.mu.Lock()
		done := en.Translations["zh-Hans"] != nil
		h.inbox.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.inbox.translating.Lock() // the pass is over
	h.inbox.translating.Unlock()
	tr := en.Translations["zh-Hans"]
	if tr == nil || tr.Subject != "设计评审能改期吗？" || tr.Of != "7" {
		t.Fatalf("not translated: %+v", tr)
	}
	if zh.Translations != nil {
		t.Fatal("Chinese mail translated into Chinese")
	}
	// The list carries subject and preview, not the body.
	h.inbox.mu.Lock()
	listed := en.listed()
	h.inbox.mu.Unlock()
	if l := listed.Translations["zh-Hans"]; l == nil || l.Subject == "" || l.Text != "" {
		t.Fatalf("listed: %+v", l)
	}
	raw, _ := json.Marshal(listed)
	if !json.Valid(raw) {
		t.Fatal("bad json")
	}
	// Opening the mail takes the stored translation: a model that would answer wrongly is not asked.
	setReply(`{"subject":"错","text":"错"}`)
	got, err := h.translateMail(context.Background(), "a", 7, "INBOX", "zh-Hans")
	if err != nil || got.Subject != "设计评审能改期吗？" {
		t.Fatalf("opened: %+v %v", got, err)
	}
	// A device that turned translation off: nothing to do.
	prefs.Translate.Enabled = false
	_ = h.push.setPrefs("dev1", prefs)
	if len(h.push.mailTargets()) != 0 {
		t.Fatal("still a target")
	}
}

func TestTranslationPassOutlivesTheRefreshRequest(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	model, _ := mutableModel(t, `{"subject":"设计评审能改期吗？","text":"你好，周四我要出差。"}`)
	h.triageAI = model
	h.mail = fakeInboxMail{msgs: map[uint32]*mail.MailMessage{
		7: {MailSummary: mail.MailSummary{Subject: "Can we move the design review?"}, Text: "Hi, I'm travelling on Thursday, so I won't make the review."},
	}}
	prefs := defaultPrefs()
	prefs.Translate = TranslatePrefs{Enabled: true, Target: "zh-Hans", Mail: true}
	if err := h.push.setPrefs("dev1", prefs); err != nil {
		t.Fatal(err)
	}
	en := &InboxItem{ID: "mail:a:t:7", Source: "mail", Kind: KindMail, Title: "Can we move the design review?",
		Preview: "Hi, I'm travelling on Thursday, so I won't make the review.", Last: "7", BodyOf: "7", UpdatedAt: time.Now(),
		Mail: &InboxMail{AccountID: "a", Mailbox: "INBOX", UIDs: []uint32{7}}}
	h.inbox.items[en.ID] = en

	// The refresh request has already answered by the time the pass runs.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.translateInbox(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.inbox.mu.Lock()
		done := en.Translations["zh-Hans"] != nil
		h.inbox.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.inbox.translating.Lock()
	h.inbox.translating.Unlock()
	if tr := en.Translations["zh-Hans"]; tr == nil || tr.Subject != "设计评审能改期吗？" {
		t.Fatalf("pass stopped with the request: %+v", tr)
	}
}
