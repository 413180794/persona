package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The `brief` item of a routine's conversation (docs/protocol.md "Conversation feed"): the facts of a run with facts on,
// written when the run is sent whether or not the Lark bot is on, so the apps show the same fixed part as the Lark card.

const feedBrief = "brief" // ref: the routine's task id; body: briefBody

// briefBody is a `brief` item's body. The rows are the facts the run started with: all the cards waiting and all the tasks
// to verify, and the newest of done (10) and failed (8), whose totals say how many there were. Texts the hub writes (a
// waiting card's line) are in the bot's language.
type briefBody struct {
	Title string    `json:"title"` // the routine's name
	At    time.Time `json:"at"`
	Hours int       `json:"hours"` // how far back done / failed look
	// WaitingUnknown: the cards waiting were not read yet (the hub had just started): `waiting` is empty for want of knowing.
	WaitingUnknown bool            `json:"waiting_unknown,omitempty"`
	Waiting        []briefBodyRow  `json:"waiting"`
	Done           []briefBodyDone `json:"done"`
	DoneTotal      int             `json:"done_total"`
	Failed         []todayFailed   `json:"failed"`
	FailedTotal    int             `json:"failed_total"`
	Accept         []briefBodyRow  `json:"accept"`
	// Progress are the tasks being worked on, the longest running first (at most 8); stage is plan / develop / review / merge / deploy / verify, "" when not known.
	Progress      []briefBodyProgress `json:"progress"`
	ProgressTotal int                 `json:"progress_total"`
}

// briefBodyRow is a row of what waits: a card (waiting) with its line, or a task to verify (accept) with its title.
type briefBodyRow struct {
	ID      string    `json:"id"`                // the card (waiting), the task (accept)
	Title   string    `json:"title"`             // waiting: what the card asks, in a line; accept: the task's title
	Detail  string    `json:"detail,omitempty"`  // accept: what changed, in the words of its "Please verify" card
	From    string    `json:"from,omitempty"`    // who asked
	Link    string    `json:"link,omitempty"`    // the page in the web app (empty when hub.json has no public_url)
	WaitedS int64     `json:"waited_s"`          // seconds waited when the run started
	Nudged  bool      `json:"nudged,omitempty"`  // waiting: the bot nudged under its card
	AskedAt time.Time `json:"asked_at,omitzero"` // accept: when the round's "Please verify" card was asked; POST /api/v1/todos/{id}/accept takes it back
}

// briefBodyDone is a task closed in the window.
type briefBodyDone struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// briefBodyProgress is a task being worked on.
type briefBodyProgress struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Stage string `json:"stage,omitempty"`
}

// body is the facts as a `brief` item's body.
func (f *briefFacts) body(s *Service, title string) briefBody {
	b := briefBody{Title: title, At: f.At, Hours: int(briefWindow / time.Hour), WaitingUnknown: f.WaitingUnknown, DoneTotal: f.DoneTotal,
		Failed: f.Failed, FailedTotal: f.FailedTotal, Waiting: []briefBodyRow{}, Done: []briefBodyDone{}, Accept: []briefBodyRow{}, ProgressTotal: f.ProgressTotal, Progress: []briefBodyProgress{}}
	if b.Failed == nil {
		b.Failed = []todayFailed{}
	}
	row := func(r boardRow) briefBodyRow {
		return briefBodyRow{ID: r.id, Title: r.summary, From: r.from, Link: r.link, WaitedS: int64(r.waited / time.Second), Nudged: r.nudged}
	}
	for _, r := range f.Waiting {
		b.Waiting = append(b.Waiting, row(r))
	}
	for _, r := range f.Accept {
		x := row(r)
		x.Title, x.Detail, x.AskedAt = r.id, f.Changed[r.id], r.asked
		if t, ok := s.todoNow(r.id); ok {
			x.Title = t.Title
		}
		b.Accept = append(b.Accept, x)
	}
	for _, d := range f.Done {
		b.Done = append(b.Done, briefBodyDone{ID: d.ID, Title: d.Title, At: d.At})
	}
	for _, p := range f.Progress {
		b.Progress = append(b.Progress, briefBodyProgress{ID: p.ID, Title: p.Title, Stage: p.Stage})
	}
	return b
}

// briefFeed writes a run's facts into its conversation, with the time it was sent as its time: after the run's ⏰ message.
func (s *Service) briefFeed(session, task, title string, f *briefFacts) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.appendFeed(ctx, session, feedBrief, task, f.body(s, title)); err != nil {
		s.logf("brief: the feed item of %s: %v", task, err)
	}
}

func (s *Service) briefRoutes() {
	// POST /api/v1/todos/{id}/accept {"asked_at"}: the Accept button of a `brief` item. It verifies the task when asked_at is
	// the round that waits now; a button of an earlier round, or a task already verified, is 409.
	s.handle("POST /api/v1/todos/{id}/accept", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot change todos"))
			return
		}
		var in struct {
			AskedAt string `json:"asked_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AskedAt == "" {
			apierr.Write(w, http.StatusBadRequest, errors.New("asked_at is required"))
			return
		}
		if !s.acceptedFromBrief(r.Context(), r.PathValue("id"), in.AskedAt) {
			apierr.Write(w, http.StatusConflict, i18n.Err("assistant-feishu_cards.already_handled"))
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
}
