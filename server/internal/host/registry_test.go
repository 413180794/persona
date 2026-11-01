package host

import (
	"context"
	"errors"
	"testing"
)

// stub is a Host of some kind that does nothing.
type stub struct{ kind string }

func (s stub) Kind() string                                          { return s.kind }
func (stub) Version(context.Context) string                          { return "" }
func (stub) Snapshot(context.Context) (*Layout, error)               { return &Layout{}, nil }
func (stub) Agents(context.Context) ([]Agent, error)                 { return nil, nil }
func (stub) Agent(context.Context, PaneRef) (*Agent, error)          { return &Agent{}, nil }
func (stub) Read(context.Context, PaneRef, ReadOpts) (string, error) { return "", nil }
func (stub) SendText(context.Context, PaneRef, string) error         { return nil }
func (stub) SendKeys(context.Context, PaneRef, ...string) error      { return nil }
func (stub) Prompt(context.Context, PaneRef, string) error           { return nil }
func (stub) PaneProcesses(context.Context, PaneRef) ([]int, error)   { return nil, nil }
func (stub) PaneEnv() string                                         { return "" }

func TestPaneRefStoredForm(t *testing.T) {
	r := NewRegistry(stub{Herdr}, stub{"orca"})
	for _, c := range []struct {
		stored string
		want   PaneRef
	}{
		{"w1:p2", PaneRef{Herdr, "w1:p2"}}, // herdr ids keep their colon and no prefix
		{"p7", PaneRef{Herdr, "p7"}},
		{"", PaneRef{Herdr, ""}},
		{"herdr:w1:p2", PaneRef{Herdr, "herdr:w1:p2"}}, // herdr is never a prefix
		{"orca:tab1:leaf2", PaneRef{"orca", "tab1:leaf2"}},
		{"tmux:%3", PaneRef{Herdr, "tmux:%3"}}, // not a registered host
	} {
		got := r.Ref(c.stored)
		if got != c.want {
			t.Errorf("Ref(%q) = %+v, want %+v", c.stored, got, c.want)
		}
		if got.String() != c.stored {
			t.Errorf("Ref(%q).String() = %q", c.stored, got.String())
		}
	}
	if h, ref := r.Pane("orca:x"); h == nil || h.Kind() != "orca" || ref.ID != "x" {
		t.Errorf("Pane(orca:x) = %v, %+v", h, ref)
	}
	if h, _ := NewRegistry(stub{"orca"}).Pane("w1:p1"); h != nil {
		t.Errorf("a herdr pane without a herdr host has no host, got %v", h)
	}
}

func TestRegistryKeepsFirstOfAKind(t *testing.T) {
	a, b := stub{Herdr}, stub{Herdr}
	r := NewRegistry(a, nil, b, stub{"orca"})
	if len(r.All()) != 2 || r.All()[0].Kind() != Herdr || r.All()[1].Kind() != "orca" {
		t.Fatalf("All = %v", r.All())
	}
	var nilReg *Registry
	if nilReg.Get(Herdr) != nil || nilReg.All() != nil {
		t.Fatal("a nil registry has no hosts")
	}
}

func TestMark(t *testing.T) {
	base := errors.New("agent_not_found: no agent")
	err := Mark(base, ErrNoAgent)
	if err.Error() != base.Error() || !errors.Is(err, ErrNoAgent) || !errors.Is(err, base) {
		t.Fatalf("Mark = %v", err)
	}
	if Mark(nil, ErrNoAgent) != nil {
		t.Fatal("Mark(nil) must stay nil")
	}
}
