package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/tgerr"
	"github.com/nwverse/persona/server/internal/connector"
)

const (
	exampleID   = "1234567"
	exampleHash = "0123456789abcdef0123456789abcd9f" // made up: 32 hex digits
)

// memSecrets is the hub's store, in memory and in the clear (the hub seals it).
type memSecrets struct {
	mu   sync.Mutex
	data []byte
}

func (m *memSecrets) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, nil
}
func (m *memSecrets) Save(_ context.Context, d []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append([]byte(nil), d...)
	return nil
}
func (m *memSecrets) Delete(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = nil
	return nil
}
func (m *memSecrets) stored(t *testing.T) stored {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	var s stored
	if m.data != nil {
		if err := json.Unmarshal(m.data, &s); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// fakeTelegram answers like Telegram for one made-up account: code 12345, 2FA password example-password.
type fakeTelegram struct {
	mu        sync.Mutex
	signedIn  bool
	twoFA     bool
	loggedOut bool
	dials     int
	// The account's conversations and their messages (archive_test.go).
	dialogs []dialog
	history map[string][]message // peer key -> messages, oldest first
	reads   []string             // "<key> after <minID>": what History was asked
	updated func()
	// Every request goes through the connector's gate, as gotd's do (client.go): sent is each one's method, start and
	// end; latency is how long one takes; fail is what Telegram answers instead (a flood wait).
	sent     []sentRequest
	inFlight int
	most     int // the most requests in flight at once
	latency  time.Duration
	fail     func(method string) error
}

type sentRequest struct {
	method     string
	start, end time.Time
}

// rpc is one request to the made-up Telegram, through the gate.
func (c *fakeClient) rpc(ctx context.Context, method string) error {
	return c.g.do(ctx, func(context.Context) error {
		f := c.f
		f.mu.Lock()
		f.inFlight++
		f.most = max(f.most, f.inFlight)
		start, latency, fail := time.Now(), f.latency, f.fail
		f.mu.Unlock()
		time.Sleep(latency)
		f.mu.Lock()
		f.inFlight--
		f.sent = append(f.sent, sentRequest{method, start, time.Now()})
		f.mu.Unlock()
		if fail != nil {
			return fail(method)
		}
		return nil
	})
}

func (f *fakeTelegram) requests() []sentRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentRequest(nil), f.sent...)
}

func (f *fakeTelegram) dial(ctx context.Context, c creds, store sessionStore, g *gate, updated func()) (client, error) {
	f.mu.Lock()
	f.dials++
	f.mu.Unlock()
	if data, err := store.LoadSession(ctx); err == nil && string(data) == "example session" {
		f.mu.Lock()
		f.signedIn = true
		f.mu.Unlock()
	}
	done := make(chan struct{})
	go func() { <-ctx.Done(); close(done) }()
	f.mu.Lock()
	f.updated = updated
	f.mu.Unlock()
	return &fakeClient{f: f, store: store, done: done, creds: c, g: g}, nil
}

type fakeClient struct {
	f     *fakeTelegram
	store sessionStore
	done  chan struct{}
	creds creds
	g     *gate
}

func (c *fakeClient) Done() <-chan struct{} { return c.done }

// Check: like Telegram, made-up credentials connect fine and are refused when asked about.
func (c *fakeClient) Check(ctx context.Context) error {
	if err := c.rpc(ctx, "Check"); err != nil {
		return err
	}
	if c.creds.hash != exampleHash {
		return tgerr.New(400, "API_ID_INVALID")
	}
	return nil
}

func (c *fakeClient) SendCode(ctx context.Context, phone string) (string, string, error) {
	if err := c.rpc(ctx, "SendCode"); err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(phone, "+") {
		return "", "", tgerr.New(400, "PHONE_NUMBER_INVALID")
	}
	return "hash-1", "验证码发到了你别的 Telegram 设备上", nil
}

func (c *fakeClient) SignIn(ctx context.Context, _, code, hash string) (string, error) {
	if err := c.rpc(ctx, "SignIn"); err != nil {
		return "", err
	}
	if hash != "hash-1" || code != "12345" {
		return "", tgerr.New(400, "PHONE_CODE_INVALID")
	}
	if c.f.twoFA {
		return "", errPasswordNeeded
	}
	return c.signIn(ctx)
}

func (c *fakeClient) signIn(ctx context.Context) (string, error) {
	c.f.mu.Lock()
	c.f.signedIn = true
	c.f.mu.Unlock()
	return "示例用户（@example）", c.store.StoreSession(ctx, []byte("example session"))
}

func (c *fakeClient) PasswordHint(ctx context.Context) (string, error) {
	return "示例提示", c.rpc(ctx, "PasswordHint")
}

func (c *fakeClient) Password(ctx context.Context, p string) (string, error) {
	if err := c.rpc(ctx, "Password"); err != nil {
		return "", err
	}
	if p != "example-password" {
		return "", tgerr.New(400, "PASSWORD_HASH_INVALID")
	}
	return c.signIn(ctx)
}

func (c *fakeClient) Me(ctx context.Context) (string, bool, error) {
	if err := c.rpc(ctx, "Me"); err != nil {
		return "", false, err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if !c.f.signedIn {
		return "", false, nil
	}
	return "示例用户（@example）", true, nil
}

func (c *fakeClient) Dialogs(ctx context.Context, _ int) ([]dialog, error) {
	if err := c.rpc(ctx, "Dialogs"); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	return append([]dialog(nil), c.f.dialogs...), nil
}

func (c *fakeClient) History(ctx context.Context, d dialog, minID int, since time.Time, max int) ([]message, error) {
	if err := c.rpc(ctx, "History"); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.f.reads = append(c.f.reads, fmt.Sprintf("%s after %d", d.Peer.key(), minID))
	var out []message
	for _, m := range c.f.history[d.Peer.key()] {
		if m.ID > minID && (minID != 0 || !m.At.Before(since)) {
			out = append(out, m)
		}
	}
	if len(out) > max { // the newest max
		out = out[len(out)-max:]
	}
	return out, nil
}

func (c *fakeClient) Older(ctx context.Context, d dialog, before, limit int) ([]message, error) {
	if err := c.rpc(ctx, "Older"); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	var out []message
	for _, m := range c.f.history[d.Peer.key()] {
		if before == 0 || m.ID < before {
			out = append(out, m)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (c *fakeClient) Search(ctx context.Context, query string, in *dialog, limit int) ([]found, error) {
	if err := c.rpc(ctx, "Search"); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	var out []found
	for _, d := range c.f.dialogs {
		if in != nil && d.Peer.key() != in.Peer.key() {
			continue
		}
		h := c.f.history[d.Peer.key()]
		for i := len(h) - 1; i >= 0; i-- {
			if strings.Contains(h[i].Text, query) && len(out) < limit {
				out = append(out, found{Dialog: d, Message: h[i]})
			}
		}
	}
	return out, nil
}

func (c *fakeClient) UnreadMentions(ctx context.Context, d dialog, limit int) ([]message, error) {
	if err := c.rpc(ctx, "UnreadMentions"); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	var out []message
	for _, m := range c.f.history[d.Peer.key()] {
		if m.Mentioned && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}

func (c *fakeClient) LogOut(ctx context.Context) error {
	if err := c.rpc(ctx, "LogOut"); err != nil {
		return err
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.f.signedIn, c.f.loggedOut = false, true
	return nil
}

// quickPace makes the gate's gap 0–1 ms for a test that does not measure it.
func quickPace(t *testing.T) {
	t.Helper()
	paceMin, paceMax = 0, time.Millisecond
	t.Cleanup(func() { paceMin, paceMax = 500*time.Millisecond, time.Second })
}

func running(t *testing.T, sec *memSecrets, f *fakeTelegram) (*Conn, context.Context) {
	t.Helper()
	quickPace(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := newConn(sec, f.dial)
	go c.Run(ctx)
	return c, ctx
}

func waitState(t *testing.T, c *Conn, state string) connector.Status {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if st := c.Status(context.Background()); st.State == state {
			return st
		}
	}
	t.Fatalf("status stayed %+v, want %s", c.Status(context.Background()), state)
	return connector.Status{}
}

// step runs one sign-in step and keeps the prompt's JSON, to check no secret ever shows in one.
func step(t *testing.T, c *Conn, ctx context.Context, seen *[]string, name string, v map[string]string) connector.Prompt {
	t.Helper()
	p, err := c.LoginStep(ctx, name, v)
	if err != nil {
		t.Fatalf("step %q: %v", name, err)
	}
	b, _ := json.Marshal(p)
	*seen = append(*seen, string(b))
	return p
}

// The whole sign-in, every mistake asked again with a reason: the API credentials (checked against Telegram), the
// phone, the code, the 2FA password. What is kept is the user's credentials and the session; the API hash shows
// nowhere but its last 4 characters.
func TestSignIn(t *testing.T) {
	sec := &memSecrets{}
	f := &fakeTelegram{twoFA: true}
	c, ctx := running(t, sec, f)
	waitState(t, c, connector.StateNotSignedIn)
	var seen []string

	if p := step(t, c, ctx, &seen, "", nil); p.Step != stepCredentials || !strings.Contains(p.Hint, "my.telegram.org") || !p.Fields[1].Secret {
		t.Fatalf("start %+v", p)
	}
	if p := step(t, c, ctx, &seen, stepCredentials, map[string]string{"api_id": "abc", "api_hash": exampleHash}); p.Step != stepCredentials || p.Error == "" {
		t.Fatalf("a malformed id %+v", p)
	}
	if p := step(t, c, ctx, &seen, stepCredentials, map[string]string{"api_id": exampleID, "api_hash": strings.Repeat("0", 32)}); p.Step != stepCredentials || !strings.Contains(p.Error, "不认") {
		t.Fatalf("credentials Telegram refuses %+v", p)
	}
	if sec.stored(t).hasCredentials() {
		t.Fatal("refused credentials were kept")
	}
	if p := step(t, c, ctx, &seen, stepCredentials, map[string]string{"api_id": exampleID, "api_hash": exampleHash}); p.Step != stepPhone {
		t.Fatalf("after the credentials %+v", p)
	}
	if st := sec.stored(t); st.APIID != 1234567 || st.APIHash != exampleHash {
		t.Fatalf("kept %+v", st)
	}
	if p := step(t, c, ctx, &seen, stepPhone, map[string]string{"phone": "555 0123"}); p.Step != stepPhone || !strings.Contains(p.Error, "手机号") {
		t.Fatalf("a phone without its country code %+v", p)
	}
	if p := step(t, c, ctx, &seen, stepPhone, map[string]string{"phone": "+1 555 0123"}); p.Step != stepCode || p.Hint == "" {
		t.Fatalf("after the phone %+v", p)
	}
	if p := step(t, c, ctx, &seen, stepCode, map[string]string{"code": "00000"}); p.Step != stepCode || p.Error != "验证码不对" {
		t.Fatalf("a wrong code %+v", p)
	}
	if p := step(t, c, ctx, &seen, stepCode, map[string]string{"code": "12345"}); p.Step != stepPassword || !strings.Contains(p.Hint, "示例提示") || !p.Fields[0].Secret {
		t.Fatalf("2FA %+v", p)
	}
	if p := step(t, c, ctx, &seen, stepPassword, map[string]string{"password": "nope"}); p.Step != stepPassword || p.Error != "密码不对" {
		t.Fatalf("a wrong password %+v", p)
	}
	p := step(t, c, ctx, &seen, stepPassword, map[string]string{"password": "example-password"})
	if !p.Done || p.Account != "示例用户（@example）" {
		t.Fatalf("signed in %+v", p)
	}
	st := c.Status(ctx)
	if st.State != connector.StateOK || st.Via != "api_id 1234567 · api_hash ••••cd9f" {
		t.Fatalf("status %+v", st)
	}
	if kept := sec.stored(t); string(kept.Session) != "example session" || kept.Account == "" {
		t.Fatalf("kept %+v", kept)
	}
	sb, _ := json.Marshal(st)
	seen = append(seen, string(sb))
	for _, s := range seen {
		if strings.Contains(s, exampleHash) || strings.Contains(s, "example session") || strings.Contains(s, "example-password") || strings.Contains(s, "555 0123") { // the number entered (the placeholder is another)
			t.Fatalf("a secret shows: %s", s)
		}
	}
}

// What the hub kept signs in by itself after a restart: no step to go through again.
func TestRestoredSession(t *testing.T) {
	raw, _ := json.Marshal(stored{APIID: 1234567, APIHash: exampleHash, Session: []byte("example session")})
	sec := &memSecrets{data: raw}
	c, ctx := running(t, sec, &fakeTelegram{})
	st := waitState(t, c, connector.StateOK)
	if st.Account != "示例用户（@example）" || !strings.HasSuffix(st.Via, "cd9f") {
		t.Fatalf("status %+v", st)
	}
	if p, _ := c.LoginStep(ctx, "", nil); !p.Done {
		t.Fatalf("already signed in, the sign-in starts over: %+v", p)
	}
}

// Signing out ends the session at Telegram and forgets the credentials and the session; signing in starts at the
// credentials again.
func TestLogout(t *testing.T) {
	raw, _ := json.Marshal(stored{APIID: 1234567, APIHash: exampleHash, Session: []byte("example session")})
	sec := &memSecrets{data: raw}
	f := &fakeTelegram{}
	c, ctx := running(t, sec, f)
	waitState(t, c, connector.StateOK)
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.loggedOut {
		t.Fatal("the session was not ended at Telegram")
	}
	if sec.data != nil {
		t.Fatalf("still kept: %s", sec.data)
	}
	waitState(t, c, connector.StateNotSignedIn)
	if p, _ := c.LoginStep(ctx, "", nil); p.Step != stepCredentials {
		t.Fatalf("after signing out %+v", p)
	}
}

// Telegram's flood limit is said for people, with how long to wait.
func TestFloodWait(t *testing.T) {
	why, ok := userError(tgerr.New(420, "FLOOD_WAIT_30"))
	if !ok || !strings.Contains(why, "30 秒") {
		t.Fatalf("%q %v", why, ok)
	}
	if got := explain(tgerr.New(400, "PHONE_CODE_INVALID")); got != "PHONE_CODE_INVALID" {
		t.Fatalf("explain %q", got)
	}
}
