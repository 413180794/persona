package voice

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

// Handing a task to the Persona tab's main conversation (the assistant with the user's data and skills) and hearing
// back when it is done: the voice model is quick but can do little, the main conversation can do a lot, slowly. The
// answer comes back into the voice conversation if it is still open; it stays in the main one anyway.

type delegation struct {
	Status   string // done | needs_you | timeout | failed | aborted
	Text     string // done: the main conversation's last reply; failed: what went wrong
	Machine  string
	Session  string
	Finished time.Time
}

type delegateOptions struct {
	poll    time.Duration
	timeout time.Duration
	now     func() time.Time
}

var defaultDelegate = delegateOptions{poll: 3 * time.Second, timeout: 20 * time.Minute, now: time.Now}

// delegate sends message to the main conversation (starting one when there is none) and waits for its turn to end.
func delegate(ctx context.Context, hub *hubClient, message string, o delegateOptions) delegation {
	machine, session, err := hub.assistant(ctx)
	if err != nil {
		return delegation{Status: "failed", Text: err.Error()}
	}
	sent := o.now()
	if session != "" {
		err = hub.send(ctx, machine, session, message)
	} else {
		session, err = hub.startMain(ctx, message)
	}
	d := delegation{Machine: machine, Session: session}
	if err != nil {
		d.Status, d.Text = "failed", err.Error()
		return d
	}
	// Its turn is over once it stops working after the message: its updated_at moves past the send, and the state is
	// no longer working. needs_you means it asked something (ask_user, a permission) the phone has to answer.
	worked := false
	for o.now().Sub(sent) < o.timeout {
		select {
		case <-ctx.Done():
			d.Status = "aborted"
			return d
		case <-time.After(o.poll):
		}
		s, err := hub.session(ctx, machine, session)
		if err != nil || s == nil {
			continue
		}
		if s.State == "working" {
			worked = true
			continue
		}
		if !worked && s.UpdatedAt.Before(sent.Add(-time.Second)) {
			continue
		}
		if s.State == "needs_you" {
			d.Status = "needs_you"
			return d
		}
		d.Status, d.Text = "done", s.LastText
		return d
	}
	d.Status = "timeout"
	return d
}

// report is what the voice model is told when a delegated task is back: something it then says.
func report(task string, d delegation) string {
	return prompts.RenderVoiceReport(prompts.VoiceReport{Task: clipRunes(task, 60), Status: d.Status, Text: clipRunes(d.Text, 4000)})
}
