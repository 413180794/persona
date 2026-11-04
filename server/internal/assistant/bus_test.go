package assistant

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The hub relays assistantwire.Routes (it does not run this package): they are exactly the ones the assistant serves.
func TestRoutesAreTheWireOnes(t *testing.T) {
	got, want := newTestService(t, newFakeHub()).Routes(), slices.Clone(assistantwire.Routes)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the assistant serves %v\nthe hub relays %v", got, want)
	}
}

// The hub's coded errors get their kind back over the bus (the assistant's code checks errors.Is), with the hub's
// message; others stay as they came.
func TestBusErrorKinds(t *testing.T) {
	for code, is := range map[string]func(error) bool{
		assistantwire.CodeNoDevice:       func(err error) bool { return errors.Is(err, ErrNoDevice) },
		assistantwire.CodeMachineOffline: func(err error) bool { return errors.Is(err, apierr.ErrOffline) },
		assistantwire.CodeNodeClosed:     func(err error) bool { return errors.Is(err, rpc.ErrClosed) },
		assistantwire.CodeStaleSnapshot:  func(err error) bool { return errors.Is(err, ErrStaleSnapshot) },
		assistantwire.CodeModelPaused:    ai.IsPaused,
	} {
		err := busError(&rpc.Error{Code: code, Message: "说明"})
		if !is(err) || err.Error() != "说明" {
			t.Errorf("%s: %v (%T)", code, err, err)
		}
	}
	var re *rpc.Error
	if err := busError(&rpc.Error{Code: "permission_denied", Message: "不行"}); !errors.As(err, &re) || re.Code != "permission_denied" {
		t.Errorf("a node's own refusal: %v", err)
	}
	if err := busError(context.DeadlineExceeded); err != context.DeadlineExceeded {
		t.Errorf("not coded: %v", err)
	}
	if busError(nil) != nil {
		t.Error("nil")
	}
}
