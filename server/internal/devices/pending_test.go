package devices

import (
	"context"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
)

// The computers waiting to be confirmed are the hub's (machine_trust is in its database): the manager knows them from
// the handover only, and knows nothing before it or while the hub could not read them.
func TestPendingMachineIDsFollowTheHandover(t *testing.T) {
	d := New(Options{})
	ctx := context.Background()
	if _, err := d.PendingMachineIDs(ctx); err == nil {
		t.Fatal("known before the hub handed anything over")
	}
	d.Configure(devicewire.Config{Pending: []string{"mac"}})
	if got, err := d.PendingMachineIDs(ctx); err != nil || !slices.Equal(got, []string{"mac"}) {
		t.Fatalf("pending = %v, %v", got, err)
	}
	d.Configure(devicewire.Config{PendingUnknown: true})
	if _, err := d.PendingMachineIDs(ctx); err == nil {
		t.Fatal("known while the hub could not read its list")
	}
	d.Configure(devicewire.Config{})
	if got, err := d.PendingMachineIDs(ctx); err != nil || len(got) != 0 {
		t.Fatalf("pending = %v, %v", got, err)
	}
}
