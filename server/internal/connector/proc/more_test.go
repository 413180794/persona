package proc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

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
	cfg := program(t, &memSecrets{}, "JARVIS_PROC_KIND=settings")
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
	cfg := program(t, &memSecrets{}, "JARVIS_PROC_KIND=flaky-settings")
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
