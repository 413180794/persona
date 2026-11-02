package connector

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// runs counts how often a connector's Run started and ended.
type runs struct {
	fake
	started, ended chan struct{}
}

func (r *runs) Run(ctx context.Context) {
	r.started <- struct{}{}
	<-ctx.Done()
	r.ended <- struct{}{}
}

func waitOn(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s", what)
	}
}

// Switched off, a connector is "disabled" to every pipe (no tools, its tools refuse), stops running, and lists as
// not enabled; switched on, it is itself again and starts again.
func TestSetEnabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &runs{fake: fake{id: "example", status: Status{State: StateOK}, watch: func(ctx context.Context, _ func(string)) error {
		<-ctx.Done()
		return nil
	}}, started: make(chan struct{}, 4), ended: make(chan struct{}, 4)}
	r := NewRegistry(nil)
	_ = r.Add(c)
	r.Start(ctx, func(string, string) {}, func(string, ...any) {})
	waitOn(t, c.started, "first start")

	if err := r.SetEnabled("example", false); err != nil {
		t.Fatal(err)
	}
	waitOn(t, c.ended, "stop when switched off")
	if st := StatusOf(ctx, c); st.State != StateDisabled {
		t.Fatalf("status %+v", st)
	}
	if len(r.Tools(ctx)) != 0 {
		t.Fatal("a disabled connector offers tools")
	}
	if res := r.Call(ctx, "example_read", json.RawMessage(`{}`), nil); !res.IsError {
		t.Fatal("a disabled connector's tool ran")
	}
	if infos := r.Infos(ctx); infos[0].Enabled || infos[0].Status != StateDisabled {
		t.Fatalf("infos %+v", infos)
	}

	if err := r.SetEnabled("example", true); err != nil {
		t.Fatal(err)
	}
	waitOn(t, c.started, "start when switched on")
	if st := StatusOf(ctx, c); st.State != StateOK || len(r.Tools(ctx)) == 0 || !r.Infos(ctx)[0].Enabled {
		t.Fatalf("back on: %+v", st)
	}
	_ = r.SetEnabled("example", true) // on twice: still one Run
	select {
	case <-c.started:
		t.Fatal("started twice")
	case <-time.After(50 * time.Millisecond):
	}
	if err := r.SetEnabled("nobody", false); err == nil {
		t.Fatal("switched off a connector that is not there")
	}
}

// Switched off before Start (the hub restores the user's choice first), it never runs.
func TestDisabledBeforeStartDoesNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &runs{fake: fake{id: "example", watch: func(ctx context.Context, _ func(string)) error { <-ctx.Done(); return nil }},
		started: make(chan struct{}, 4), ended: make(chan struct{}, 4)}
	r := NewRegistry(nil)
	_ = r.Add(c)
	_ = r.SetEnabled("example", false)
	r.Start(ctx, func(string, string) {}, func(string, ...any) {})
	select {
	case <-c.started:
		t.Fatal("a disabled connector ran")
	case <-time.After(50 * time.Millisecond):
	}
}

// signIn is a connector with Login; says is one that names its capabilities itself (a program's adapter).
type signIn struct{ plain }

func (signIn) LoginStep(context.Context, string, map[string]string) (Prompt, error) {
	return Prompt{}, nil
}
func (signIn) Logout(context.Context) error { return nil }

type says struct{ fake }

func (says) Capabilities() []string { return []string{"status", "archive"} }

func TestCapabilitiesOfLoginAndCapabler(t *testing.T) {
	if got := Capabilities(signIn{}); !slices.Equal(got, []string{"login"}) {
		t.Fatalf("login: %v", got)
	}
	// It implements every method, but says it has only these: the list shown is what it says.
	if got := Capabilities(&says{fake: fake{id: "example"}}); !slices.Equal(got, []string{"status", "archive"}) {
		t.Fatalf("capabler: %v", got)
	}
}
