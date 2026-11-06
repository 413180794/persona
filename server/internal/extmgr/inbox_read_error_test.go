package extmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

// failingInbox is an inbox connector whose Collect fails with err.
type failingInbox struct {
	feedConnector
	err error
}

func (f *failingInbox) Collect(context.Context) ([]connector.InboxThing, error) { return nil, f.err }

// A connector whose Collect fails is in the round, marked with a short code (never the error's words, which may hold
// an address or a token), not left out: the manager keeps what it had and the apps say it is behind
// (#4801). A held round over it keeps the earlier things and the failure.
func TestInboxRoundSaysASourceCouldNotBeRead(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		err  error
		want string
	}{
		{errors.New("dial https://user:secret@example.com: connection refused"), assistantwire.ReadFailed},
		{context.DeadlineExceeded, assistantwire.ReadTimeout},
		{&rpc.Error{Code: "rate_limited", Message: "slow down"}, assistantwire.ReadRateLimited},
	} {
		h := newTestManager(t)
		h.connectors = connector.NewRegistry(nil)
		if err := h.addConnector(&failingInbox{err: c.err}); err != nil {
			t.Fatal(err)
		}
		src := h.readConnectors(ctx)
		if len(src) != 1 || src[0].ID != "example" || src[0].ReadError != c.want || len(src[0].Things) != 0 {
			t.Fatalf("%v: %+v", c.err, src)
		}
		if strings.Contains(src[0].ReadError, "secret") {
			t.Fatalf("the error's words went into the round: %q", src[0].ReadError)
		}
	}
	old := assistantwire.InboxRound{Names: map[string]string{"example": "Example"},
		Sources: []assistantwire.InboxSource{{ID: "example", Things: []connector.InboxThing{{Thread: "t1", Last: "1"}}}}}
	n := assistantwire.InboxRound{Names: old.Names,
		Sources: []assistantwire.InboxSource{{ID: "example", Things: []connector.InboxThing{}, ReadError: assistantwire.ReadFailed}}}
	got := mergeRounds(old, n)
	if len(got.Sources) != 1 || len(got.Sources[0].Things) != 1 || got.Sources[0].ReadError != assistantwire.ReadFailed {
		t.Fatalf("held round over a failed read: %+v", got.Sources)
	}
}
