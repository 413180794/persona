package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// named is a source of a name only (what Ingest needs).
type named string

func (n named) Name() string                                   { return string(n) }
func (named) Chats(context.Context, int) ([]SourceChat, error) { return nil, nil }
func (named) Messages(context.Context, SourceChat, string, int) ([]SourceMessage, string, bool, error) {
	return nil, "", false, nil
}
func (named) Download(context.Context, string, string) error { return nil }

func TestHashHandle(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	same := [][2]Handle{
		{{Kind: HandleEmail, Value: "Alice@Example.com "}, {Kind: HandleEmail, Value: "alice@example.com"}},
		{{Kind: HandlePhone, Value: "+1 (555) 010-0100"}, {Kind: HandlePhone, Value: "+15550100100"}},
	}
	for _, p := range same {
		if a, b := HashHandle(key, p[0]), HashHandle(key, p[1]); a == "" || a != b {
			t.Errorf("%v and %v: %q %q", p[0], p[1], a, b)
		}
	}
	if HashHandle(key, Handle{Kind: HandleGitHub, Value: "alice"}) == HashHandle(key, Handle{Kind: HandleTelegram, Value: "alice"}) {
		t.Error("kinds share a hash")
	}
	if h := HashHandle(key, Handle{Kind: HandleEmail, Value: "alice@example.com"}); !strings.HasPrefix(h, "email:") || len(h) != len("email:")+32 ||
		strings.Contains(h, "alice") {
		t.Errorf("hash %q", h)
	}
	if HashHandle(nil, Handle{Kind: HandleEmail, Value: "a@b"}) != "" || HashHandle(key, Handle{Kind: HandlePhone, Value: "+ ()"}) != "" {
		t.Error("hashed without a key or a value")
	}
	if HashHandle([]byte("another key, also 32 bytes long!"), Handle{Kind: HandleEmail, Value: "a@b"}) == HashHandle(key, Handle{Kind: HandleEmail, Value: "a@b"}) {
		t.Error("the key does not matter")
	}
	for _, vector := range []struct {
		hint Handle
		want string
	}{
		{Handle{Kind: HandlePhone, Value: handleTestPhone}, handleTestPhoneHash},
		{Handle{Kind: HandlePhone, Value: handleTestPhoneE164}, handleTestPhoneHash},
		{Handle{Kind: HandleEmail, Value: handleTestEmail}, handleTestEmailHash},
	} {
		if got := HashHandle(key, vector.hint); got != vector.want {
			t.Errorf("HMAC differs from independently calculated vector: %q", got)
		}
	}
}

// Only exact HMACs belong in handles.json. Full raw/normalized hints and handle structures must not be exposed in
// any other file or kb_get output; legitimate timestamps containing the old short sentinels must remain valid.
func TestPeopleKeepHashedHandles(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: st, HandleKey: []byte("0123456789abcdef0123456789abcdef")}
	ctx := context.Background()
	// Preserve the timestamp collision that used to produce four false positives, without aging out retention.
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Add(555019900 * time.Nanosecond)
	started := time.Now().UTC()
	imID, mailID := PersonID("imessage", "0a1b2c"), PersonID("mail", "sender-2")
	// Source IDs are opaque so a legitimate identity cannot mask a leaked email hint.
	if _, err := e.Ingest(ctx, named("imessage"), SourceChat{ID: "c1", Name: "示例"}, []SourceMessage{
		{ID: "1", SenderID: "0a1b2c", SenderName: "张三", Text: "在吗", At: at, Type: "TEXT",
			SenderHandles: []Handle{{Kind: HandlePhone, Value: handleTestPhone}, {Kind: HandleEmail, Value: handleTestEmail}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Ingest(ctx, named("mail"), SourceChat{ID: "acc-2", Name: "张三 · 邮件"}, []SourceMessage{
		{ID: "2", SenderID: "sender-2", SenderName: "张三", Text: "附件", At: at, Type: "TEXT",
			SenderHandles: []Handle{{Kind: HandleEmail, Value: strings.ToLower(handleTestEmail)}}},
	}); err != nil {
		t.Fatal(err)
	}
	// Seen again with different formatting: neither a duplicate nor a different hash may be added.
	if _, err := e.Ingest(ctx, named("mail"), SourceChat{ID: "acc-2", Name: "张三 · 邮件"}, []SourceMessage{
		{ID: "3", SenderID: "sender-2", SenderName: "张三", Text: "再发一次", At: at.Add(time.Minute), Type: "TEXT",
			SenderHandles: []Handle{{Kind: HandleEmail, Value: strings.ToUpper(handleTestEmail)}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Ingest(ctx, named("imessage"), SourceChat{ID: "c1", Name: "示例"}, []SourceMessage{
		{ID: "4", SenderID: "0a1b2c", SenderName: "张三", Text: "再发一次", At: at, Type: "TEXT",
			SenderHandles: []Handle{{Kind: HandlePhone, Value: handleTestPhoneE164}}},
	}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string][]string{imID: {handleTestPhoneHash, handleTestEmailHash}, mailID: {handleTestEmailHash}} {
		raw, err := json.Marshal(st.PersonHandles(id))
		if err != nil {
			t.Fatal(err)
		}
		if err := checkStoredHandles(raw, want); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	for id, wantAt := range map[string]time.Time{ChatID("imessage", "c1"): at, ChatID("mail", "acc-2"): at.Add(time.Minute)} {
		chat, ok := st.Chat(id)
		if !ok || !chat.LastAt.Equal(wantAt) || chat.Created.Before(started) || chat.Created.After(time.Now().UTC()) {
			t.Fatalf("%s has invalid timestamp metadata: %+v", id, chat)
		}
	}
	for _, id := range []string{imID, mailID} {
		person, ok := st.Person(id)
		if !ok || !person.LastSeen.Equal(at) {
			t.Fatalf("%s did not preserve the legitimate fractional timestamp: %+v", id, person)
		}
	}
	// Without a key no handle file may exist, not even an empty one.
	e.HandleKey = nil
	noKeyID := PersonID("github", "sender-3")
	if _, err := e.Ingest(ctx, named("github"), SourceChat{ID: "pr1", Name: "PR"}, []SourceMessage{
		{ID: "5", SenderID: "sender-3", SenderName: "alice", Text: "LGTM", At: at, Type: "TEXT",
			SenderHandles: []Handle{{Kind: HandleGitHub, Value: "alice"}, {Kind: HandlePhone, Value: handleTestPhone}, {Kind: HandleEmail, Value: handleTestEmail}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Path(KindPerson, noKeyID, "handles.json")); !os.IsNotExist(err) {
		t.Fatalf("handle file exists without a key, or lookup failed: %v", err)
	}
	wantFiles := map[string][]string{
		filepath.Join(KindPerson, imID, "handles.json"):   {handleTestPhoneHash, handleTestEmailHash},
		filepath.Join(KindPerson, mailID, "handles.json"): {handleTestEmailHash},
	}
	seenFiles := 0
	if err := filepath.WalkDir(st.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(st.Dir, p)
		if err != nil {
			return err
		}
		if filepath.Base(p) == "handles.json" {
			want, ok := wantFiles[rel]
			if !ok {
				return fmt.Errorf("unexpected handle file %s", rel)
			}
			seenFiles++
			return checkStoredHandles(raw, want)
		}
		if err := checkHandleText(rel); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if filepath.Ext(p) == ".json" {
			err = checkHandleJSON(raw)
		} else {
			err = checkHandleText(string(raw))
		}
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seenFiles != len(wantFiles) {
		t.Fatalf("checked %d handle files, want %d", seenFiles, len(wantFiles))
	}
	for kind, ids := range map[string][]string{
		KindPerson: {imID, mailID, noKeyID},
		KindChat:   {ChatID("imessage", "c1"), ChatID("mail", "acc-2"), ChatID("github", "pr1")},
	} {
		for _, id := range ids {
			detail, err := e.Get(kind, id, 1000)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkHandleJSON(raw); err != nil {
				t.Fatalf("kb_get %s/%s: %v", kind, id, err)
			}
		}
	}
}
