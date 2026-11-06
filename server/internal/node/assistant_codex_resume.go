package node

import (
	"context"
	"errors"
	"time"

	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/proto"
)

// codexApprovalOwner also routes connector confirmations through the current
// connection. Reconnecting an approval never retries the tool being approved.
type codexApprovalOwner struct{}

func (c *codexConn) askResumable(ctx context.Context, ix proto.Interaction) (proto.Resolution, bool, error) {
	if c.n.keeper == nil {
		return c.p.ownerOr(c.n).askOnce(ctx, ix)
	}
	ix.Resumable = true
	ctx, cancel := context.WithDeadline(ctx, ix.ExpiresAt)
	defer cancel()
	var until time.Time
	for {
		if err := ctx.Err(); err != nil {
			return proto.Resolution{}, false, err
		}
		res, ok, err := c.p.ownerOr(c.n).askOnce(ctx, ix)
		if !errors.Is(err, errNodeGone) {
			return res, ok, err // explicit refusal and dismissal are final
		}
		if until.IsZero() {
			until = time.Now().Add(nodeclient.ResumeWait)
		}
		remaining := time.Until(until)
		if remaining <= 0 {
			return res, false, err
		}
		timer := time.NewTimer(min(nodeclient.ResumeRetry, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return res, false, ctx.Err()
		case <-c.p.exited:
			timer.Stop()
			return res, false, context.Canceled
		case <-timer.C:
		}
	}
}
