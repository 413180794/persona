package proc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/rpc"
)

// settingsProgram has the user's settings, settles its inbox, hears of computers and takes a sign-in source: what the
// hub's own GitHub connector does, for a program taking its place. Its status's account says what it was given.
type settingsProgram struct {
	mu       sync.Mutex
	options  json.RawMessage
	machines []string
	auth     serve.AuthSource
}

func (*settingsProgram) ID() string   { return "example" }
func (*settingsProgram) Name() string { return "Example" }

func (*settingsProgram) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapInbox, connector.CapSettle,
		connector.CapOptions, connector.CapMachines}}
}

func (p *settingsProgram) Status(context.Context) connector.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return connector.Status{State: connector.StateOK, Account: fmt.Sprintf("%s|%v|%s", p.options, p.machines, p.auth.Machine)}
}

func (*settingsProgram) Collect(context.Context) ([]connector.InboxThing, error) { return nil, nil }
func (*settingsProgram) Reply(context.Context, connector.InboxReply) error       { return nil }

func (*settingsProgram) Settle(_ context.Context, threads []string) map[string]string {
	out := map[string]string{}
	for _, t := range threads {
		if t == "old" {
			out[t] = "closed"
		}
	}
	return out
}

func (p *settingsProgram) Options() json.RawMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.options == nil {
		return json.RawMessage(`{"ci":"mine"}`)
	}
	return p.options
}

func (p *settingsProgram) SetOptions(raw json.RawMessage) error {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return errors.New("not settings")
	}
	p.mu.Lock()
	p.options = raw
	p.mu.Unlock()
	return nil
}

func (p *settingsProgram) MachineOnline(id string) {
	p.mu.Lock()
	p.machines = append(p.machines, id)
	p.mu.Unlock()
}

func (p *settingsProgram) Auth(context.Context) serve.AuthView {
	p.mu.Lock()
	defer p.mu.Unlock()
	return serve.AuthView{Source: p.auth.Source, Machine: p.auth.Machine, Scopes: []string{"repo"}, ScopesKnown: true}
}

func (p *settingsProgram) SetAuth(ctx context.Context, a serve.AuthSource) (serve.AuthView, error) {
	p.mu.Lock()
	p.auth = a
	p.mu.Unlock()
	return p.Auth(ctx), nil
}

func TestSettingsAndSignInSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := program(t, &memSecrets{}, "PERSONA_PROC_KIND=settings")
	cfg.Manifest = (&settingsProgram{}).Manifest()
	c := New(cfg)
	// Set before the program runs: kept, and given to it when it starts.
	if err := c.SetOptions(json.RawMessage(`{"ci":"all"}`)); err != nil {
		t.Fatal(err)
	}
	if v, err := c.SetAuth(ctx, serve.AuthSource{Source: "machine", Machine: "m1", MachineName: "Mac"}); err != nil || v.Machine != "m1" {
		t.Fatal(v, err)
	}
	go c.Run(ctx)
	waitUntil(t, "the program has the settings and the source", func() bool {
		return c.Status(ctx).Account == `{"ci":"all"}|[]|m1`
	})
	if got := string(c.Options()); got != `{"ci":"all"}` {
		t.Fatal("options as the program keeps them:", got)
	}
	if err := c.SetOptions(json.RawMessage(`[1]`)); err == nil {
		t.Fatal("the program refuses what it cannot use")
	}
	if err := c.SetOptions(json.RawMessage(`{"ci":"none"}`)); err != nil || string(c.Options()) != `{"ci":"none"}` {
		t.Fatal(err, string(c.Options()))
	}
	if got := c.Settle(ctx, []string{"old", "new"}); len(got) != 1 || got["old"] != "closed" {
		t.Fatal("settled:", got)
	}
	c.MachineOnline("m2")
	if v, err := c.Auth(ctx); err != nil || v.Source != "machine" || !v.ScopesKnown {
		t.Fatal(v, err)
	}
	c.refresh(ctx)
	waitUntil(t, "a computer's coming", func() bool {
		c.refresh(ctx)
		return c.Status(ctx).Account == `{"ci":"none"}|[m2]|m1`
	})
	if _, ok := connector.As[connector.Machines](c); !ok {
		t.Fatal("declared machines: the registry tells it")
	}
}

// flakySettingsProgram refuses its first sign-in source and its first settings (still loading), takes them after
// that, and has defaults of its own until it is given the user's.
type flakySettingsProgram struct {
	*settingsProgram
	authCalls, optionsCalls int
}

func (p *flakySettingsProgram) Options() json.RawMessage {
	if o := p.settingsProgram.Options(); string(o) != `{"ci":"mine"}` {
		return o
	}
	return json.RawMessage(`{"default":true}`)
}

func (p *flakySettingsProgram) SetOptions(raw json.RawMessage) error {
	p.mu.Lock()
	p.optionsCalls++
	first := p.optionsCalls == 1
	p.mu.Unlock()
	if first {
		return errors.New("still loading")
	}
	return p.settingsProgram.SetOptions(raw)
}

func (p *flakySettingsProgram) SetAuth(ctx context.Context, a serve.AuthSource) (serve.AuthView, error) {
	p.mu.Lock()
	p.authCalls++
	first := p.authCalls == 1
	p.mu.Unlock()
	if first {
		return serve.AuthView{}, errors.New("still loading")
	}
	return p.settingsProgram.SetAuth(ctx, a)
}

// A program that refuses the user's settings or sign-in source at start does not get its defaults kept in their
// place: they stay pending and go to it on a later refresh.
func TestSyncOptionsRetriesAfterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := program(t, &memSecrets{}, "PERSONA_PROC_KIND=flaky-settings")
	cfg.Manifest = (&settingsProgram{}).Manifest()
	c := New(cfg)
	if err := c.SetOptions(json.RawMessage(`{"user":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetAuth(ctx, serve.AuthSource{Source: "machine", Machine: "m1"}); err != nil {
		t.Fatal(err)
	}
	go c.Run(ctx)
	waitUntil(t, "the first refresh", func() bool { return c.Status(ctx).State == connector.StateOK })
	if got := string(c.Options()); got != `{"user":1}` {
		t.Fatal("the user's settings after a refused start:", got)
	}
	waitUntil(t, "the program has the settings and the source", func() bool {
		c.refresh(ctx)
		return c.Status(ctx).Account == `{"user":1}|[]|m1`
	})
	if got := string(c.Options()); got != `{"user":1}` {
		t.Fatal("options as the program keeps them:", got)
	}
}

// optionsPeer is a running program, over pipes, that takes settings but fails to say which it keeps (options.get
// timing out, or the program dying between the two calls).
func optionsPeer(t *testing.T) *rpc.Peer {
	t.Helper()
	return pipedProgram(t, func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == serve.MOptionsSet {
			return struct{}{}, nil
		}
		return nil, &rpc.Error{Code: "timeout", Message: "no answer"}
	})
}

// Settings the program took are kept when it then fails to say which it keeps: they are what the hub stores and
// what it gives the program again after a restart, not the settings from before.
func TestSetOptionsKeptWhenReadFails(t *testing.T) {
	var logs []string
	c := New(Config{ID: "example", Name: "Example", Manifest: (&settingsProgram{}).Manifest(),
		Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	c.options = json.RawMessage(`{"ci":"all"}`)
	c.peer = optionsPeer(t)
	if err := c.SetOptions(json.RawMessage(`{"ci":"none"}`)); err != nil {
		t.Fatal(err)
	}
	if got := string(c.Options()); got != `{"ci":"none"}` {
		t.Fatal("options after a failed read:", got)
	}
	if len(logs) != 1 {
		t.Fatal("the failed read is logged:", logs)
	}
	// The program restarts: the hub gives it the user's settings again.
	c.sent = false
	var resent json.RawMessage
	c.syncOptions(context.Background(), func(method string, params, _ any) error {
		if method == serve.MOptionsSet {
			resent = params.(serve.OptionsParams).Options
			return nil
		}
		return &rpc.Error{Code: serve.CodeNoCapability, Message: "no options"}
	})
	if string(resent) != `{"ci":"none"}` {
		t.Fatal("re-sent after a restart:", string(resent))
	}
	if len(logs) != 1 {
		t.Fatal("no_capability is not logged:", logs)
	}
}

// Without machines declared, a program is not told of computers (connector.As); without options it has none.
func TestUndeclaredSettings(t *testing.T) {
	c := New(Config{ID: "example", Name: "Example", Manifest: (&exampleProgram{}).Manifest()})
	if _, ok := connector.As[connector.Machines](c); ok {
		t.Fatal("machines not declared")
	}
	if _, ok := connector.As[connector.Options](c); ok {
		t.Fatal("options not declared")
	}
	if _, ok := connector.As[connector.InboxSettler](c); ok {
		t.Fatal("settle not declared")
	}
}

func TestGhToken(t *testing.T) {
	ctx := context.Background()
	params := json.RawMessage(`{"machine":"m1","host":"github.com"}`)
	c := New(Config{ID: "github", Name: "GitHub"})
	var re *rpc.Error
	if _, err := c.Handle(ctx, serve.MGhToken, params); !errors.As(err, &re) || re.Code != serve.CodeNotPermitted {
		t.Fatal("not granted:", err)
	}
	c = New(Config{ID: "github", Name: "GitHub", GhToken: func(_ context.Context, machine, host string) (string, error) {
		if machine != "m1" || host != "github.com" {
			return "", &rpc.Error{Code: serve.CodeNoMachine, Message: "no such machine"}
		}
		return "t0ken", nil
	}})
	out, err := c.Handle(ctx, serve.MGhToken, params)
	if err != nil || out.(serve.GhTokenResult).Token != "t0ken" {
		t.Fatal(out, err)
	}
	if _, err := c.Handle(ctx, serve.MGhToken, json.RawMessage(`{"machine":"m9"}`)); !errors.As(err, &re) || re.Code != serve.CodeNoMachine {
		t.Fatal("the code goes to the program:", err)
	}
}

// pipedProgram is a program reached over an in-process rpc pair: answer handles the hub's calls on the program's
// side. The returned peer is the hub's end, given to the connector as the running program's.
func pipedProgram(t *testing.T, answer rpc.Handler) *rpc.Peer {
	t.Helper()
	toProg, fromHub := io.Pipe()
	toHub, fromProg := io.Pipe()
	hub := rpc.New("h", fromHub)
	prog := rpc.New("p", fromProg)
	prog.OnRequest = answer
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, toHub) }()
	go func() { _ = prog.Serve(ctx, toProg) }()
	t.Cleanup(func() {
		cancel()
		_ = toProg.Close()
		_ = toHub.Close()
		hub.Close(rpc.ErrClosed)
		prog.Close(rpc.ErrClosed)
	})
	return hub
}

// A computer's coming that the program refuses is logged, and one it does not answer ends with the connector's run
// instead of outliving it.
func TestMachineOnline(t *testing.T) {
	t.Run("refused is logged", func(t *testing.T) {
		deadline := time.After(10 * time.Second)
		logs := make(chan string, 4)
		c := New(Config{ID: "example", Name: "Example", Logf: func(f string, a ...any) { logs <- fmt.Sprintf(f, a...) }})
		peer := pipedProgram(t, func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: "busy", Message: "cannot take it now"}
		})
		_, stop := c.hold(context.Background())
		defer stop()
		c.mu.Lock()
		c.peer = peer
		c.mu.Unlock()
		c.MachineOnline("m1")
		select {
		case line := <-logs:
			if !strings.Contains(line, serve.MMachineOnline) || !strings.Contains(line, "cannot take it now") {
				t.Fatal("log line:", line)
			}
		case <-deadline:
			t.Fatal("the refusal was not logged")
		}
	})
	t.Run("ends with the run", func(t *testing.T) {
		deadline := time.After(10 * time.Second)
		started, canceled := make(chan struct{}), make(chan struct{})
		c := New(Config{ID: "example", Name: "Example"})
		peer := pipedProgram(t, func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		})
		// The program hears when the hub stops waiting, so its handler shows whether MachineOnline's call ended.
		peer.Cancels = map[string]bool{serve.MMachineOnline: true}
		_, stop := c.hold(context.Background())
		defer stop()
		c.mu.Lock()
		c.peer = peer
		c.mu.Unlock()
		c.MachineOnline("m1")
		select {
		case <-started:
		case <-deadline:
			t.Fatal("the program was not asked")
		}
		stop()
		select {
		case <-canceled:
		case <-deadline:
			t.Fatal("the call outlived the connector's run")
		}
	})
}

// Settings given while the run ends under the call stay pending for the next run instead of being lost.
func TestSetOptionsEndsWithTheRun(t *testing.T) {
	deadline := time.After(10 * time.Second)
	started := make(chan struct{})
	c := New(Config{ID: "example", Name: "Example"})
	peer := pipedProgram(t, func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	_, stop := c.hold(context.Background())
	defer stop()
	c.mu.Lock()
	c.peer = peer
	c.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- c.SetOptions(json.RawMessage(`{"ci":"none"}`)) }()
	select {
	case <-started:
	case <-deadline:
		t.Fatal("the program was not asked")
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("SetOptions:", err)
		}
	case <-deadline:
		t.Fatal("the call outlived the connector's run")
	}
	c.mu.Lock()
	options, sent := string(c.options), c.sent
	c.mu.Unlock()
	if options != `{"ci":"none"}` || sent {
		t.Fatalf("settings %s sent %v, want them pending", options, sent)
	}
}
