package assistant

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
)

// "Please verify" (#2768, docs/bots.md "Please verify"): once a task's work is deployed, the bot asks the user in Feishu to try it —
// three lines: what changed, where to look (a link straight to the page), how to check — with "Pass" on the card. "Pass"
// (the button, or a reply saying so) marks the task verified; any other reply to the card is written into the todo's
// decisions and sent to the agent that did the work. One still not accepted is asked again the next morning.

const acceptanceKey = "assistant.acceptance"

// acceptMorningHour is when, in the user's zone, the cards not accepted the day before are posted again, when the
// morning briefing (the "Morning briefing" routine) is off or gone; while it is on, its own time.
const acceptMorningHour = 9

type acceptAsk struct {
	MessageID string   `json:"message_id"`
	Messages  []string `json:"messages,omitempty"` // every card asking it (the first and the reminders)
	// Earlier are the cards of the task's rounds before this one (#4853): dead, so a press or a reply on one is answered
	// "already handled" and never accepts this round or reaches the chat.
	Earlier    []string  `json:"earlier,omitempty"`
	MachineID  string    `json:"machine_id"`
	SessionID  string    `json:"session_id"`
	Changed    string    `json:"changed"`
	Link       string    `json:"link,omitempty"`
	How        string    `json:"how"`
	AskedAt    time.Time `json:"asked_at"`
	RemindedAt time.Time `json:"reminded_at,omitzero"`
	AcceptedAt time.Time `json:"accepted_at,omitzero"`
}

type acceptBook struct {
	mu     sync.Mutex
	loaded bool
	asks   map[string]*acceptAsk // by todo id
}

// loadAcceptLocked reads the asks once and reports whether they are in memory; s.accept.mu is held. A failed read
// leaves them unloaded (the next look tries again): writing the book after it would put an empty or partial map over
// the stored cards, whose replies, reminders and briefing lines would then be lost.
func (s *Service) loadAcceptLocked(ctx context.Context) bool {
	b := &s.accept
	if b.loaded {
		return true
	}
	asks := map[string]*acceptAsk{}
	if st := s.store(); st != nil {
		if _, err := st.Setting(ctx, acceptanceKey, &asks); err != nil {
			s.logf("acceptance: load: %v", err)
			return false
		}
		if asks == nil {
			asks = map[string]*acceptAsk{}
		}
	}
	b.asks, b.loaded = asks, true
	return true
}

// saveAcceptLocked writes the asks, once they were read (see loadAcceptLocked).
func (s *Service) saveAcceptLocked(ctx context.Context) {
	if !s.accept.loaded {
		return
	}
	if st := s.store(); st != nil {
		if err := st.PutSetting(ctx, acceptanceKey, s.accept.asks); err != nil {
			s.logf("acceptance: save: %v", err)
		}
	}
}

var urlRe = regexp.MustCompile(`https?://[^\s<>()\[\]"'，。；、）]+`)

// askAcceptance posts task t's "Please verify" card once (reply: what its agent said last, on machine/session).
func (s *Service) askAcceptance(t Todo, machine, session, reply string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		b.mu.Unlock()
		return
	}
	var earlier []string
	if old := b.asks[t.ID]; old != nil && !acceptAskStale(old, t) {
		b.mu.Unlock()
		return
	} else if old != nil {
		// asked before the task's latest reopen: the new round gets its own card, the old ones stay dead
		earlier = lastOf(slices.Concat(old.Earlier, old.Messages), acceptEarlierMost)
		delete(b.asks, t.ID)
	}
	b.asks[t.ID] = &acceptAsk{Earlier: earlier} // claimed: a second turn ending meanwhile does not ask twice
	b.mu.Unlock()
	release := func() {
		b.mu.Lock()
		delete(b.asks, t.ID)
		b.mu.Unlock()
	}
	f, ready := s.feishuFeatures(ctx)
	if !ready || !f.Reports {
		release()
		return
	}
	a := s.acceptLines(ctx, t, machine, session, reply)
	a.MachineID, a.SessionID, a.AskedAt = machine, session, time.Now().UTC()
	mid, err := s.botPost(ctx, "", s.acceptCard(ctx, t, a, false), a.Changed)
	if err != nil {
		s.logf("task %s: acceptance: %v", t.ID, err)
		release()
		return
	}
	a.MessageID, a.Messages, a.Earlier = mid, []string{mid}, earlier
	b.mu.Lock()
	b.asks[t.ID] = &a
	s.saveAcceptLocked(ctx)
	b.mu.Unlock()
}

// acceptLines are the card's three lines: the small model reads the task and the reply; without it (or when it
// answers nothing usable) the task's title, its page and a plain check.
func (s *Service) acceptLines(ctx context.Context, t Todo, machine, session, reply string) acceptAsk {
	l := s.botLang(ctx)
	web := s.webBase(ctx)
	var links []string
	add := func(u string) {
		if u != "" && !slices.Contains(links, u) {
			links = append(links, u)
		}
	}
	for _, u := range urlRe.FindAllString(reply, -1) {
		add(strings.TrimRight(u, ".,;:!?"))
	}
	add(todoLink(web, t.ID))
	if web != "" && session != "" {
		add(sessionLink(web, machine, session, false))
	}
	a := acceptAsk{Changed: clip(oneLine(t.Title), 40), Link: todoLink(web, t.ID), How: i18n.T(l, "assistant-reports.accept_how_default")}
	var out struct {
		Changed string `json:"changed"`
		Link    string `json:"link"`
		How     string `json:"how"`
	}
	input := prompts.RenderAcceptanceInput(prompts.Acceptance{Title: t.Title, Prompt: clipMiddle(strings.TrimSpace(t.Notes), 800),
		Reply: clipMiddle(reply, 2000), Links: links})
	if err := s.purposeModel("inbox.judge").JSON(ctx, prompts.RenderAcceptance(), input, &out); err != nil {
		s.logf("task %s: acceptance lines: %v (plain ones)", t.ID, err)
		return a
	}
	if c := clip(oneLine(out.Changed), 60); c != "" {
		a.Changed = c
	}
	if slices.Contains(links, strings.TrimSpace(out.Link)) {
		a.Link = strings.TrimSpace(out.Link) // only one it was given: never a link it made up
	}
	if h := clip(oneLine(out.How), 120); h != "" {
		a.How = h
	}
	return a
}

// acceptCard is the "Please verify" card (again: the morning's reminder), or how it ended (accepted).
func (s *Service) acceptCard(ctx context.Context, t Todo, a acceptAsk, again bool) *feishubot.Card {
	l := s.botLang(ctx)
	head := "assistant-reports.accept_title"
	if again {
		head = "assistant-reports.accept_again"
	}
	title := i18n.T(l, head, i18n.Args{"task": clip(oneLine(t.Title), 30)})
	c := feishubot.NewCard("🧪 "+title, feishubot.Purple, title)
	link := i18n.T(l, "assistant-reports.accept_no_link")
	if a.Link != "" {
		link = "[" + feishubot.EscapeMarkdown(clip(a.Link, 80)) + "](" + a.Link + ")"
	}
	c.Markdown(i18n.T(l, "assistant-reports.accept_lines", i18n.Args{"changed": feishubot.EscapeMarkdown(a.Changed), "link": link,
		"how": feishubot.EscapeMarkdown(a.How)}))
	if !a.AcceptedAt.IsZero() {
		return c.Note(i18n.T(l, "assistant-reports.accepted"))
	}
	bs := []feishubot.Button{feishubot.Btn(i18n.T(l, "assistant-reports.accept_pass"), feishubot.Primary, map[string]any{"a": "accept", "id": t.ID})}
	if a.Link != "" {
		bs = append(bs, feishubot.LinkBtn(i18n.T(l, "assistant-reports.accept_open"), a.Link))
	}
	c.Buttons(bs...)
	return c.Note(i18n.T(l, "assistant-reports.accept_reply_hint"))
}

// acceptEarlierMost is how many cards of earlier rounds a task's ask remembers.
const acceptEarlierMost = 20

// lastOf is list's last n.
func lastOf(list []string, n int) []string {
	if len(list) > n {
		return list[len(list)-n:]
	}
	return list
}

// acceptFor is the todo whose "Please verify" card message id is (or the thread it is in), "" when none; earlier when the card
// is one of an earlier round of that task (Earlier).
func (s *Service) acceptFor(ids ...string) (todo string, earlier bool) {
	b := &s.accept
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.loadAcceptLocked(context.Background()) {
		return "", false
	}
	for todo, a := range b.asks {
		for _, id := range ids {
			if id != "" && slices.Contains(a.Messages, id) {
				return todo, false
			}
		}
	}
	for todo, a := range b.asks {
		for _, id := range ids {
			if id != "" && slices.Contains(a.Earlier, id) {
				return todo, true
			}
		}
	}
	return "", false
}

// acceptAskLocked returns todo id's stored ask (nil: none); callers only read its immutable AskedAt.
func (s *Service) acceptAskLocked(id string) *acceptAsk {
	b := &s.accept
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.asks[id]
}

// passWords accepts bare approval, optionally prefixed with the Chinese word for "verify" and followed by one or two
// Chinese completion particles (as in "ok" plus a particle) or a stop. Question particles stay out, and
// both full-width and ASCII question marks are neither particles nor stops, so questions still go to the agent, and
// negations ("not passed", "can't pass", "no good") never match because the regex anchors on the approval word.
var passWords = regexp.MustCompile(`(?i)^\s*(验收)?(通过|没问题|可以|好的?|ok|okay|lgtm|pass(ed)?|approved?|👍)(?:[了啦呀咯喽呗哒啊]{1,2})?\s*[。.!！]?\s*$`)

// acceptReply answers a reply to a "Please verify" card (true: it was one): "Pass" marks it verified, anything else is the
// user's change, written into the todo's decisions and sent to its agent.
func (s *Service) acceptReply(ctx context.Context, messageID, text string, ids ...string) bool {
	id, earlier := s.acceptFor(ids...)
	if id == "" {
		return false
	}
	l := s.botLang(ctx)
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if earlier { // a card of a round before the one asking now: answered, it neither accepts nor changes this round
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	t, ok := s.todoNow(id)
	if passWords.MatchString(text) {
		// Approval on a pre-reopen card is answered but neither accepts nor reopens; a gone todo keeps old behavior.
		if ok {
			if a := s.acceptAskLocked(id); a != nil && acceptAskStale(a, t) {
				s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
				return true
			}
		}
		if s.accepted(ctx, id, "") {
			s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accepted"))
		}
		return true
	}
	if !ok {
		s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_gone"))
		return true
	}
	if a := s.acceptAskLocked(id); a != nil && acceptAskStale(a, t) { // old round's change is answered, not routed
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	if st := s.todoDB(); st != nil {
		at := decisionNow()
		if ok, err := s.insertDecision(ctx, st, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: id,
			Question: i18n.T(l, "assistant-reports.accept_question"), Choice: decisionText(text), Source: db.DecisionManual,
			SourceMessage: "feishu-" + messageID, CreatedAt: at}); err != nil {
			s.logf("task %s: acceptance decision: %v", id, err)
		} else if ok {
			s.decisionsChanged(ctx, id, at)
		}
	}
	// Not accepted: the work goes on. A task the hub (or the user) closed meanwhile is open again before its agent is
	// told, so the turn the change starts is followed (receipt, judgment, the next "Please verify") and the task shows as in
	// progress, not done (#3457). It stays open when the send fails: the change is still to be made.
	if t.StartError == "" {
		if err := s.reopenTodo(ctx, id); err != nil {
			s.logf("task %s: acceptance reopen: %v", id, err)
		}
	}
	msg := i18n.T(l, "assistant-reports.accept_to_agent", i18n.Args{"feedback": text})
	if _, err := s.routeToTask(ctx, t.Origin, id, msg); err != nil {
		s.logf("task %s: acceptance to agent: %v", id, err)
		s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_not_sent", i18n.Args{"error": clip(oneLine(err.Error()), 120)}))
		return true
	}
	// Changed again: the next deploy asks again.
	b := &s.accept
	b.mu.Lock()
	delete(b.asks, id)
	s.saveAcceptLocked(ctx)
	b.mu.Unlock()
	s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_sent"))
	return true
}

// accepted marks task id verified; false when already handled, never asked, or asked before the latest reopen. card is
// the message pressed ("" from a reply, which acceptFor already matched, or when Feishu does not say): a card that is
// not this round's (an earlier round's, once the next one has its own) accepts nothing (#4853).
func (s *Service) accepted(ctx context.Context, id, card string) bool {
	t, todoOK := s.todoNow(id)
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		b.mu.Unlock()
		return false
	}
	a, ok := b.asks[id]
	if !ok || a.MessageID == "" || !a.AcceptedAt.IsZero() || (todoOK && acceptAskStale(a, t)) || (card != "" && card != a.MessageID && !slices.Contains(a.Messages, card)) {
		b.mu.Unlock()
		return false
	}
	a.AcceptedAt = time.Now().UTC()
	cp := *a
	s.saveAcceptLocked(ctx)
	b.mu.Unlock()
	s.setTaskStage(ctx, id, "verify", "")
	s.threadPost(id, threadEvent{kind: threadTodoAccepted}) // and the todo's chat thread
	if t, ok := s.todoNow(id); ok {
		for _, mid := range cp.Messages {
			if err := s.botUpdateCard(ctx, mid, s.acceptCard(ctx, t, cp, false)); err != nil {
				s.logf("task %s: acceptance card: %v", id, err)
			}
		}
	}
	return true
}

// acceptLoop posts again, each morning, the cards not accepted by then (once a day each).
func (s *Service) acceptLoop(ctx context.Context) {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.remindAcceptance(time.Now())
		}
	}
}

func (s *Service) remindAcceptance(now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	loc := s.location(ctx)
	local := now.In(loc)
	hh, mm := s.briefingClock(ctx)
	if local.Hour()*60+local.Minute() < hh*60+mm {
		return
	}
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		b.mu.Unlock()
		return
	}
	var due []string
	seen := map[string]time.Time{} // each due card's AskedAt: the round the sweep judges
	for id, a := range b.asks {
		if a.MessageID != "" && a.AcceptedAt.IsZero() && a.AskedAt.Before(today) && a.RemindedAt.Before(today) {
			due = append(due, id)
			seen[id] = a.AskedAt
		}
	}
	b.mu.Unlock()
	if len(due) == 0 {
		return
	}
	if f, ready := s.feishuFeatures(ctx); !ready || !f.Reports {
		return
	}
	slices.Sort(due)
	for _, id := range due {
		t, ok := s.todoNow(id)
		if f := s.acceptSweepHook; f != nil {
			f(id)
		}
		b.mu.Lock()
		a := b.asks[id]
		if a == nil || !a.AskedAt.Equal(seen[id]) {
			// Gone, or a new round asked while the book was unlocked (the todo read is from before it): not this card.
			b.mu.Unlock()
			continue
		}
		if !ok || acceptAskEnded(t) || acceptAskStale(a, t) {
			// The todo is gone, it ended without the hub's auto-judge (the owner closed it or a review did), or the
			// card is from before its latest reopen: it can never be passed, so stop asking and drop it.
			delete(b.asks, id)
			s.saveAcceptLocked(ctx)
			b.mu.Unlock()
			continue
		}
		cp := *a
		b.mu.Unlock()
		mid, err := s.botPost(ctx, "", s.acceptCard(ctx, t, cp, true), cp.Changed)
		if err != nil {
			s.logf("task %s: acceptance reminder: %v", id, err)
			return
		}
		b.mu.Lock()
		var done *acceptAsk
		if a := b.asks[id]; a != nil {
			a.RemindedAt = now.UTC()
			a.Messages = append(a.Messages, mid)
			if !a.AcceptedAt.IsZero() {
				cp := *a // passed while the reminder went out: that card says so too
				done = &cp
			}
		}
		s.saveAcceptLocked(ctx)
		b.mu.Unlock()
		if done != nil {
			if err := s.botUpdateCard(ctx, mid, s.acceptCard(ctx, t, *done, false)); err != nil {
				s.logf("task %s: acceptance card: %v", id, err)
			}
		}
	}
}

// briefingClock is when the morning briefing goes out (the enabled "Morning briefing" routine's time), else acceptMorningHour: the
// reminders come with it, and the briefing lists what waits (snapshotUnaccepted).
func (s *Service) briefingClock(ctx context.Context) (int, int) {
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return acceptMorningHour, 0
	}
	defer sc.mu.Unlock()
	for _, t := range sc.tasks {
		if t.ID != "morning" || !t.Enabled || t.Repeat != repeatDaily {
			continue
		}
		if m := hhmm.FindStringSubmatch(t.Time); m != nil {
			hh, _ := strconv.Atoi(m[1])
			mm, _ := strconv.Atoi(m[2])
			return hh, mm
		}
	}
	return acceptMorningHour, 0
}

// snapshotUnaccepted are the deployed tasks still waiting for the user to check them, the oldest eight, and how many
// there are (the snapshot's "waiting for the user to verify", which the morning briefing brings up).
func (s *Service) snapshotUnaccepted(ctx context.Context, loc *time.Location) ([]prompts.SnapshotAccept, int) {
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		b.mu.Unlock()
		return nil, 0
	}
	type ask struct {
		id string
		a  acceptAsk
	}
	var list []ask
	for id, a := range b.asks {
		if a.MessageID != "" && a.AcceptedAt.IsZero() {
			list = append(list, ask{id, *a})
		}
	}
	b.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].a.AskedAt.Before(list[j].a.AskedAt) })
	var out []prompts.SnapshotAccept
	var ended []ask
	total := 0
	for _, x := range list {
		t, ok := s.todoNow(x.id)
		if f := s.acceptSweepHook; f != nil {
			f(x.id)
		}
		if !ok {
			continue
		}
		if acceptAskEnded(t) || acceptAskStale(&x.a, t) {
			ended = append(ended, x) // closed by the owner or a review, or asked before the todo's latest reopen
			continue
		}
		total++
		if len(out) < 8 { // the oldest eight; the total says how many more
			out = append(out, prompts.SnapshotAccept{Title: clip(oneLine(t.Title), 40), Changed: x.a.Changed, Link: x.a.Link,
				Since: x.a.AskedAt.In(loc)})
		}
	}
	if len(ended) > 0 {
		b.mu.Lock()
		removed := false
		for _, x := range ended {
			// Only the card judged: a new round asked while the book was unlocked has another AskedAt and stays.
			if cur := b.asks[x.id]; cur != nil && cur.AskedAt.Equal(x.a.AskedAt) {
				delete(b.asks, x.id)
				removed = true
			}
		}
		if removed {
			s.saveAcceptLocked(ctx)
		}
		b.mu.Unlock()
	}
	return out, total
}

// acceptAskEnded says the todo a "Please verify" card waits on has ended without the hub's auto-judge: the owner closed it in
// the todos themselves (DoneBy "") or a review did (DoneByReview). Such a card can never be passed, so it is neither
// reminded nor counted. An auto-closed task (DoneByAuto) is the normal state: the judge finished it, but the owner
// still has to pass the card.
func acceptAskEnded(t Todo) bool {
	return t.DoneAt != nil && t.DoneBy != db.DoneByAuto
}

// acceptAskStale says an ask belongs to an earlier round: the todo was reopened (PATCH done:false, change feedback
// on its card, or a failed start's retry) after the ask, so the old card is dead and the next deploy asks fresh.
// A ReopenedAt the auto-judge's second close leaves is harmless (the new ask is younger); zero AskedAt is a claim.
func acceptAskStale(a *acceptAsk, t Todo) bool {
	return !a.AskedAt.IsZero() && t.ReopenedAt != nil && a.AskedAt.Before(*t.ReopenedAt)
}
