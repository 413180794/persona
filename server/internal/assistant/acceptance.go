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

// 请验收 (#2768, docs/bots.md「请验收」): once a task's work is deployed, the bot asks the user in Feishu to try it —
// three lines: what changed, where to look (a link straight to the page), how to check — with 通过 on the card. 通过
// (the button, or a reply saying so) marks the task verified; any other reply to the card is written into the todo's
// decisions and sent to the agent that did the work. One still not accepted is asked again the next morning.

const acceptanceKey = "assistant.acceptance"

// acceptMorningHour is when, in the user's zone, the cards not accepted the day before are posted again, when the
// morning briefing (the 早报 routine) is off or gone; while it is on, its own time.
const acceptMorningHour = 9

type acceptAsk struct {
	MessageID  string    `json:"message_id"`
	Messages   []string  `json:"messages,omitempty"` // every card asking it (the first and the reminders)
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

func (s *Service) loadAcceptLocked(ctx context.Context) {
	b := &s.accept
	if b.loaded {
		return
	}
	b.loaded = true
	b.asks = map[string]*acceptAsk{}
	if st := s.store(); st != nil {
		if _, err := st.Setting(ctx, acceptanceKey, &b.asks); err != nil {
			s.logf("acceptance: load: %v", err)
		}
		if b.asks == nil {
			b.asks = map[string]*acceptAsk{}
		}
	}
}

func (s *Service) saveAcceptLocked(ctx context.Context) {
	if st := s.store(); st != nil {
		if err := st.PutSetting(ctx, acceptanceKey, s.accept.asks); err != nil {
			s.logf("acceptance: save: %v", err)
		}
	}
}

var urlRe = regexp.MustCompile(`https?://[^\s<>()\[\]"'，。；、）]+`)

// askAcceptance posts task t's 请验收 card once (reply: what its agent said last, on machine/session).
func (s *Service) askAcceptance(t Todo, machine, session, reply string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b := &s.accept
	b.mu.Lock()
	s.loadAcceptLocked(ctx)
	if _, asked := b.asks[t.ID]; asked {
		b.mu.Unlock()
		return
	}
	b.asks[t.ID] = &acceptAsk{} // claimed: a second turn ending meanwhile does not ask twice
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
	a.MessageID, a.Messages = mid, []string{mid}
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

// acceptCard is the 请验收 card (again: the morning's reminder), or how it ended (accepted).
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

// acceptFor is the todo whose 请验收 card message id is (or the thread it is in), "" when none.
func (s *Service) acceptFor(ids ...string) string {
	b := &s.accept
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadAcceptLocked(context.Background())
	for todo, a := range b.asks {
		for _, id := range ids {
			if id != "" && slices.Contains(a.Messages, id) {
				return todo
			}
		}
	}
	return ""
}

var passWords = regexp.MustCompile(`(?i)^\s*(验收)?(通过|没问题|可以|好的?|ok|okay|lgtm|pass(ed)?|approved?|👍)\s*[。.!！]?\s*$`)

// acceptReply answers a reply to a 请验收 card (true: it was one): 通过 marks it verified, anything else is the
// user's change, written into the todo's decisions and sent to its agent.
func (s *Service) acceptReply(ctx context.Context, messageID, text string, ids ...string) bool {
	id := s.acceptFor(ids...)
	if id == "" {
		return false
	}
	l := s.botLang(ctx)
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if passWords.MatchString(text) {
		if s.accepted(ctx, id) {
			s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accepted"))
		}
		return true
	}
	t, ok := s.todoNow(id)
	if !ok {
		s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_gone"))
		return true
	}
	if st := s.todoDB(); st != nil {
		at := decisionNow()
		if ok, err := st.InsertTodoDecision(ctx, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: id,
			Question: i18n.T(l, "assistant-reports.accept_question"), Choice: decisionText(text), Source: db.DecisionManual,
			SourceMessage: "feishu-" + messageID, CreatedAt: at}); err != nil {
			s.logf("task %s: acceptance decision: %v", id, err)
		} else if ok {
			s.decisionsChanged(ctx, id, at)
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

// accepted marks task id verified (false: it already was, or never asked).
func (s *Service) accepted(ctx context.Context, id string) bool {
	b := &s.accept
	b.mu.Lock()
	s.loadAcceptLocked(ctx)
	a, ok := b.asks[id]
	if !ok || a.MessageID == "" || !a.AcceptedAt.IsZero() {
		b.mu.Unlock()
		return false
	}
	a.AcceptedAt = time.Now().UTC()
	cp := *a
	s.saveAcceptLocked(ctx)
	b.mu.Unlock()
	s.setTaskStage(ctx, id, "verify", "")
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
	s.loadAcceptLocked(ctx)
	var due []string
	for id, a := range b.asks {
		if a.MessageID != "" && a.AcceptedAt.IsZero() && a.AskedAt.Before(today) && a.RemindedAt.Before(today) {
			due = append(due, id)
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
		b.mu.Lock()
		a := b.asks[id]
		if !ok || a == nil {
			delete(b.asks, id) // the todo is gone
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

// briefingClock is when the morning briefing goes out (the enabled 早报 routine's time), else acceptMorningHour: the
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
// there are (the snapshot's 等用户验收的, which the morning briefing brings up).
func (s *Service) snapshotUnaccepted(ctx context.Context, loc *time.Location) ([]prompts.SnapshotAccept, int) {
	b := &s.accept
	b.mu.Lock()
	s.loadAcceptLocked(ctx)
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
	total := 0
	for _, x := range list {
		t, ok := s.todoNow(x.id)
		if !ok {
			continue
		}
		total++
		if len(out) < 8 { // the oldest eight; the total says how many more
			out = append(out, prompts.SnapshotAccept{Title: clip(oneLine(t.Title), 40), Changed: x.a.Changed, Link: x.a.Link,
				Since: x.a.AskedAt.In(loc)})
		}
	}
	return out, total
}
