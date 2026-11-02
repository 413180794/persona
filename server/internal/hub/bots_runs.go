package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/feishubot"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// A message from the paired user goes to the chat's target (bots.go botTarget) and starts a run: the hub watches the
// conversation, shows a progress card while it works (after a few seconds), and when the turn ends posts its answer
// — the last words of the turn — as a card replying to the message.

type botRun struct {
	ID        string    `json:"id"`
	MessageID string    `json:"message_id"` // the user's message it answers
	Target    botTarget `json:"target"`     // resolved: machine and session
	Label     string    `json:"label"`
	Base      int       `json:"base"` // the conversation's last seq before the message: the answer comes after it
	StartedAt time.Time `json:"started_at"`
	Card      string    `json:"card,omitempty"` // the progress card, replaced by the answer
	Reaction  string    `json:"reaction,omitempty"`
	Seen      bool      `json:"seen,omitempty"` // the conversation was seen working since
	Held      bool      `json:"held,omitempty"` // answered once; background work goes on, and its end is answered too
	// Probe is a piece of the message as sent: the answer is what comes after the user message holding it, not the
	// end of a turn that was going on when it arrived (a message waits behind the turn in progress).
	Probe string `json:"probe,omitempty"`
	// Folded: earlier messages to the same conversation that this answer covers too (sent while it worked).
	Folded []botFolded `json:"folded,omitempty"`
}

type botFolded struct {
	MessageID string `json:"message_id"`
	Reaction  string `json:"reaction,omitempty"`
}

// botProgress is a run's live state, in memory only.
type botProgress struct {
	mu       chanMutex // held while the card is being written
	steps    []string
	live     string
	dirty    bool
	updated  time.Time
	checked  time.Time // the answer was last read (while the turn was not seen to end, or the message not found)
	notFound bool      // the last read did not find the message in the conversation yet
	finished bool
	done     chan struct{}
	// checking is held by checkRun: the watch loop and a turn's end may both check at once, and two checks would post
	// the same answer twice.
	checking chanMutex
}

// chanMutex is a mutex the watch loop can try without blocking.
type chanMutex chan struct{}

func newChanMutex() chanMutex { return make(chanMutex, 1) }
func (m chanMutex) Lock()     { m <- struct{}{} }
func (m chanMutex) Unlock()   { <-m }
func (m chanMutex) TryLock() bool {
	select {
	case m <- struct{}{}:
		return true
	default:
		return false
	}
}

const (
	botProgressAfter = 5 * time.Second
	botProgressEvery = 3 * time.Second
	botProgressIdle  = 12 * time.Second // the elapsed time moves on even without new steps
	botRunMax        = 4 * time.Hour
	botHoldMax       = 2 * time.Hour
	botAnswerPiece   = 6000    // runes per card: a card holds about 30 KB
	botImageMax      = 3 << 20 // a picture's base64 must stay under the model's 5 MB
	botFileMax       = 50 << 20
	botImagesMax     = 4
)

// startRun records a run and watches it. An earlier run on the same conversation that has not answered yet is folded
// into this one: one answer covers both messages.
func (h *Hub) startRun(r *botRun) {
	b := &h.bots
	b.mu.Lock()
	var folded []*botRun
	keep := b.st.Runs[:0]
	for _, o := range b.st.Runs {
		if o.Target.MachineID == r.Target.MachineID && o.Target.SessionID == r.Target.SessionID {
			folded = append(folded, o)
			if o.Base < r.Base || r.Base == 0 {
				r.Base = o.Base
			}
			if o.StartedAt.Before(r.StartedAt) {
				r.StartedAt = o.StartedAt
			}
			r.Folded = append(append(r.Folded, o.Folded...), botFolded{MessageID: o.MessageID, Reaction: o.Reaction})
			if r.Probe == "" {
				r.Probe = o.Probe
			}
			continue
		}
		keep = append(keep, o)
	}
	b.st.Runs = append(keep, r)
	gone := map[string]*botProgress{}
	for _, o := range folded {
		if p := b.live[o.ID]; p != nil {
			gone[o.ID] = p
			delete(b.live, o.ID)
		}
	}
	h.saveBotStateLocked()
	api := b.api
	b.mu.Unlock()
	for _, o := range folded {
		ended := true
		if p := gone[o.ID]; p != nil {
			p.mu.Lock()
			ended = !p.finished // finishRun may have answered it just now: then its card holds the answer
			if ended {
				p.finished = true
				close(p.done)
			}
			p.mu.Unlock()
		}
		if ended && api != nil && o.Card != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = api.UpdateCard(ctx, o.Card, feishubot.NewCard("", "", "").Note("接着下一条消息一起回答").JSON())
			cancel()
		}
	}
	go h.watchRun(r)
}

func (h *Hub) runByID(id string) (*botRun, *botProgress) {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.st.Runs {
		if r.ID == id {
			return r, b.live[id]
		}
	}
	return nil, nil
}

// watchRun follows a run until it is answered: the conversation's items reach the hub (a watcher, like an open chat
// in the app) for the progress card, which is posted after a few seconds and rewritten every few.
func (h *Hub) watchRun(r *botRun) {
	b := &h.bots
	m := h.localMachine(r.Target.MachineID)
	if m == nil { // the machine left the configuration: nothing will answer it
		b.mu.Lock()
		b.st.Runs = slices.DeleteFunc(b.st.Runs, func(o *botRun) bool { return o == r })
		h.saveBotStateLocked()
		b.mu.Unlock()
		return
	}
	b.mu.Lock()
	if b.live == nil {
		b.live = map[string]*botProgress{}
	}
	if b.live[r.ID] != nil {
		b.mu.Unlock()
		return // already watched
	}
	prog := &botProgress{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{})}
	b.live[r.ID] = prog
	b.mu.Unlock()
	ctx := context.Background()
	m.addWatcher(ctx, r.Target.SessionID)
	defer m.removeWatcher(r.Target.SessionID)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-prog.done:
			return
		case <-t.C:
		}
		if run, _ := h.runByID(r.ID); run == nil { // dropped (unpaired, folded into a later message)
			b.mu.Lock()
			if b.live[r.ID] == prog {
				delete(b.live, r.ID)
			}
			b.mu.Unlock()
			return
		}
		h.checkRun(r.ID)
		h.progressTick(r.ID)
	}
}

// progressTick posts or rewrites the progress card when it is due.
func (h *Hub) progressTick(id string) {
	r, prog := h.runByID(id)
	if r == nil || prog == nil {
		return
	}
	api, _ := h.botAPI()
	if api == nil || !prog.mu.TryLock() {
		return
	}
	defer prog.mu.Unlock()
	working := false
	if m := h.localMachine(r.Target.MachineID); m != nil {
		s, _ := m.session(r.Target.SessionID)
		working = s.Status == proto.StatusWorking || s.Starting
	}
	b := &h.bots
	b.mu.Lock()
	now := b.clock()
	card, started, label := r.Card, r.StartedAt, r.Label
	if card == "" && !working {
		b.mu.Unlock()
		return // nothing going on to show (a held run waiting on its background work)
	}
	steps, live, dirty := append([]string{}, prog.steps...), prog.live, prog.dirty
	due := !prog.finished && (card == "" && now.Sub(started) >= botProgressAfter ||
		card != "" && (dirty && now.Sub(prog.updated) >= botProgressEvery || now.Sub(prog.updated) >= botProgressIdle))
	if due {
		prog.dirty, prog.updated = false, now
	}
	b.mu.Unlock()
	if !due {
		return
	}
	c := progressCard(label, now.Sub(started), steps, live)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if card == "" {
		mid, err := api.Reply(ctx, r.MessageID, feishubot.TypeInteractive, c.JSON(), "", false)
		if err != nil {
			h.logf("bots: progress card: %v", err)
			return
		}
		b.mu.Lock()
		r.Card = mid
		h.saveBotStateLocked()
		b.mu.Unlock()
		h.rememberBotThread(mid, r.Target, label)
		return
	}
	if err := api.UpdateCard(ctx, card, c.JSON()); err != nil {
		h.logf("bots: progress update: %v", err)
	}
}

func progressCard(label string, took time.Duration, steps []string, live string) *feishubot.Card {
	c := feishubot.NewCard("", "", "处理中…")
	c.Note("⏳ " + label + " · 处理中 · " + durationText(took))
	if n := len(steps); n > 0 {
		var b strings.Builder
		if n > 4 {
			fmt.Fprintf(&b, "…前面还有 %d 步\n", n-4)
			steps = steps[n-4:]
		}
		for _, s := range steps {
			b.WriteString("- " + s + "\n")
		}
		c.Markdown(strings.TrimSpace(b.String()))
	}
	if live = strings.TrimSpace(live); live != "" {
		r := []rune(live)
		if len(r) > 400 {
			live = "…" + string(r[len(r)-400:])
		}
		c.Markdown(feishubot.EscapeMarkdown(live))
	}
	return c
}

func durationText(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%d 秒", s)
	case s < 3600:
		return fmt.Sprintf("%d 分 %d 秒", s/60, s%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", s/3600, s%3600/60)
}

// botItems: new items of a watched conversation (machine.go): the runs on it note the steps.
func (h *Hub) botItems(machine, session string, items []proto.Item) {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.live) == 0 {
		return
	}
	for _, r := range b.st.Runs {
		if r.Target.MachineID != machine || r.Target.SessionID != session {
			continue
		}
		p := b.live[r.ID]
		if p == nil {
			continue
		}
		for _, it := range items {
			if it.Seq <= r.Base {
				continue
			}
			if s := botStep(it); s != "" {
				p.steps = append(p.steps, s)
				p.dirty = true
			}
			if it.Kind == proto.KindText && it.Role == "assistant" {
				p.live = ""
			}
		}
		if len(p.steps) > 200 {
			p.steps = p.steps[len(p.steps)-200:]
		}
	}
}

// botDelta: the text a Jarvis conversation is writing right now.
func (h *Hub) botDelta(machine, session, text string) {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.live) == 0 {
		return
	}
	for _, r := range b.st.Runs {
		if r.Target.MachineID == machine && r.Target.SessionID == session {
			if p := b.live[r.ID]; p != nil && p.live != text {
				p.live, p.dirty = text, true
			}
		}
	}
}

// botStep is one line of the progress card for a tool call.
func botStep(it proto.Item) string {
	if it.Kind != proto.KindToolUse {
		return ""
	}
	var in map[string]any
	_ = json.Unmarshal(it.ToolInput, &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	name := it.ToolName
	detail := ""
	switch name {
	case "Bash":
		name, detail = "运行", firstLine(str("command"))
	case "Read":
		name, detail = "读", path.Base(str("file_path"))
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		name, detail = "改", path.Base(str("file_path"))
	case "Grep", "Glob":
		name, detail = "搜", str("pattern")
	case "WebFetch":
		name, detail = "打开网页", str("url")
	case "WebSearch":
		name, detail = "搜网页", str("query")
	case "Task", "Agent":
		name, detail = "子任务", str("description")
	case "TodoWrite":
		name = "更新待办"
	case "Skill":
		name, detail = "技能", str("skill")
	default:
		if strings.HasPrefix(name, "mcp__") {
			if i := strings.LastIndex(name, "__"); i > 4 {
				name = name[i+2:]
			}
		}
		for _, k := range []string{"command", "cmd", "query", "path", "file_path", "url", "title"} {
			if v := str(k); v != "" {
				detail = firstLine(v)
				break
			}
		}
	}
	if detail = clip(oneLine(detail), 60); detail != "" {
		return name + " " + feishubot.Code(detail)
	}
	return name
}

// checkRun answers a run whose turn ended: the conversation is no longer working, nobody is asked anything, and it was
// seen working (or long enough has passed that it must have been).
func (h *Hub) checkRun(id string) {
	r, prog := h.runByID(id)
	if r == nil || prog == nil {
		return
	}
	m := h.localMachine(r.Target.MachineID)
	if m == nil {
		return
	}
	if !prog.checking.TryLock() {
		return // another check is on it
	}
	defer prog.checking.Unlock()
	b := &h.bots
	b.mu.Lock()
	now := b.clock()
	started, held0, seen, base, probe := r.StartedAt, r.Held, r.Seen, r.Base, r.Probe
	b.mu.Unlock()
	age := now.Sub(started)
	limit := botRunMax
	if held0 {
		limit = botHoldMax
	}
	if age > limit {
		if held0 {
			h.finishRun(r, "", "", false, 0)
		} else {
			h.finishRun(r, "", fmt.Sprintf("已经 %s 了还没完，先不等了；结果到 App 里看。", durationText(age)), false, 0)
		}
		return
	}
	s, ok := m.session(r.Target.SessionID)
	if !ok {
		if age > 3*time.Minute && m.snapshot().Online {
			h.finishRun(r, "", "这段对话不见了。", false, 0)
		}
		return
	}
	if s.Status == proto.StatusWorking || s.Starting {
		if !seen {
			b.mu.Lock()
			r.Seen = true
			b.mu.Unlock()
		}
		return
	}
	if s.Status == proto.StatusBlocked || h.hasPending(m.cfg.ID, s.SessionID) {
		return // waiting on the user: the cards are out
	}
	if !seen && age < 20*time.Second {
		return
	}
	if s.Status == proto.StatusOffline && age < time.Minute {
		return
	}
	// The end was not seen (it came before the watch, or never), or the message was not there last time: look now and
	// then, not every second.
	b.mu.Lock()
	if (!seen || prog.notFound) && now.Sub(prog.checked) < 10*time.Second {
		b.mu.Unlock()
		return
	}
	prog.checked = now
	b.mu.Unlock()
	answer, last, found, err := h.turnText(m, r.Target.SessionID, base, probe)
	if err != nil {
		h.logf("bots: read the answer: %v", err)
		return // next tick
	}
	b.mu.Lock()
	prog.notFound = !found
	b.mu.Unlock()
	if !found {
		if age < 10*time.Minute {
			return // the message waits behind the turn that just ended: it starts the next one
		}
		h.logf("bots: %s: the message never showed in the conversation; answering with its last words", r.ID)
		if answer, last, _, err = h.turnText(m, r.Target.SessionID, base, ""); err != nil {
			return
		}
	}
	held := backgroundRunning(s)
	if strings.TrimSpace(answer) == "" {
		if held0 {
			if !held && age > 10*time.Minute {
				h.finishRun(r, "", "", false, last) // its background work ended without a word: nothing more to say
			}
			return
		}
		if !seen && age < 2*time.Minute {
			return // not started yet (queued behind another message, a slow start)
		}
	}
	note := ""
	if held {
		note = fmt.Sprintf("后台还有 %d 个任务在跑，跑完再告诉你。", max(len(s.Background), 1))
	}
	h.finishRun(r, answer, note, held, last)
}

func (h *Hub) hasPending(machine, session string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.interactions {
		if p.ix.MachineID == machine && p.ix.SessionID == session {
			return true
		}
	}
	return false
}

// turnText is what the conversation answered after seq base (its last words) — after the user message holding probe
// when there is one — the last seq read, and whether that message is there yet. It reads to the end of the
// transcript, keeping only the items from that message on (or the last ones without a probe): a long turn has more
// lines than one page.
func (h *Hub) turnText(m *Machine, sid string, base int, probe string) (string, int, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	holds := func(it proto.Item) bool {
		if it.Role != "user" || it.Kind != proto.KindText {
			return false
		}
		if strings.Contains(it.Text, probe) {
			return true
		}
		if it.FullSize == 0 {
			return false
		}
		// A long message (a forwarded chat) is cut in pages, maybe before what the user wrote.
		var full proto.Item
		return m.call(ctx, "session.item", map[string]any{"session_id": sid, "item_id": it.ID}, &full) == nil && strings.Contains(full.Text, probe)
	}
	var kept []proto.Item
	found := probe == ""
	after := base
	for page := 0; page < 100; page++ {
		var res struct {
			Items     []proto.Item `json:"items"`
			MoreAfter bool         `json:"more_after"`
			ToSeq     int          `json:"to_seq"`
		}
		if err := m.call(ctx, "session.items", map[string]any{"session_id": sid, "after_seq": after, "lazy_images": true}, &res); err != nil {
			return "", base, false, err
		}
		for _, it := range res.Items {
			if probe != "" && holds(it) {
				kept, found = nil, true
			}
			if found {
				kept = append(kept, it)
			}
		}
		if probe == "" && len(kept) > 1000 { // the answer is at the end
			kept = append([]proto.Item(nil), kept[len(kept)-1000:]...)
		}
		if res.ToSeq > after {
			after = res.ToSeq
		}
		if !res.MoreAfter {
			break
		}
	}
	if !found {
		return "", after, false, nil
	}
	parts := turnAnswer(kept)
	var out []string
	for _, it := range parts {
		text := it.Text
		if it.FullSize > 0 {
			var full proto.Item
			if err := m.call(ctx, "session.item", map[string]any{"session_id": sid, "item_id": it.ID}, &full); err == nil && full.Text != "" {
				text = full.Text
			}
		}
		out = append(out, strings.TrimSpace(text))
	}
	return strings.Join(out, "\n\n"), after, true, nil
}

// turnAnswer picks a turn's answer out of its items: the assistant's texts after its last tool call, or after the last
// message it was given (a message taken mid-turn); without those, its last text.
func turnAnswer(items []proto.Item) []proto.Item {
	var tail []proto.Item
	var last *proto.Item
	for i := range items {
		it := items[i]
		switch {
		case it.Role == "assistant" && it.Kind == proto.KindText && strings.TrimSpace(it.Text) != "":
			tail = append(tail, it)
			last = &items[i]
		case it.Kind == proto.KindToolUse || it.Kind == proto.KindToolResult || it.Kind == proto.KindNotice ||
			it.Role == "user" && it.Kind == proto.KindText:
			tail = nil
		}
	}
	if len(tail) == 0 && last != nil {
		return []proto.Item{*last}
	}
	return tail
}

// finishRun posts a run's answer (into its progress card when there is one) and ends it, or — held — keeps it for
// the turn its background work starts.
func (h *Hub) finishRun(r *botRun, answer, note string, held bool, last int) {
	b := &h.bots
	b.mu.Lock()
	prog := b.live[r.ID]
	if prog == nil {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	prog.mu.Lock()
	if prog.finished {
		prog.mu.Unlock()
		return
	}
	b.mu.Lock()
	card, reaction, took, wasHeld, folded := r.Card, r.Reaction, b.clock().Sub(r.StartedAt), r.Held, r.Folded
	if held {
		// Answered; the next turn (background work reporting back) gets its own progress card and answer.
		r.Held, r.Card, r.Seen, r.StartedAt, r.Folded, r.Probe = true, "", false, b.clock().UTC(), nil, ""
		if last > r.Base {
			r.Base = last
		}
		prog.steps, prog.live, prog.dirty = nil, "", false
	} else {
		prog.finished = true
		delete(b.live, r.ID)
		b.st.Runs = slices.DeleteFunc(b.st.Runs, func(o *botRun) bool { return o == r })
		if b.answered == nil {
			b.answered = map[string]time.Time{}
		}
		for k, at := range b.answered {
			if b.clock().Sub(at) > time.Hour {
				delete(b.answered, k)
			}
		}
		b.answered[r.Target.SessionID] = b.clock()
	}
	h.saveBotStateLocked()
	b.mu.Unlock()
	defer prog.mu.Unlock()
	if !held {
		defer close(prog.done)
	}
	api, _ := h.botAPI()
	if api == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	foot := r.Label + " · 用时 " + durationText(took)
	if note != "" {
		foot += "\n" + note
	}
	quiet := strings.TrimSpace(answer) == "" && note == "" && wasHeld
	if strings.TrimSpace(answer) == "" && note == "" {
		answer = "（这一轮没有文字回答）"
	}
	pieces := feishubot.Split(feishubot.CardMarkdown(answer), botAnswerPiece)
	if quiet {
		pieces = nil
	}
	for i, piece := range pieces {
		c := feishubot.NewCard("", "", answer)
		c.Markdown(piece)
		if i == len(pieces)-1 {
			c.Note(foot)
		}
		var mid string
		var err error
		if i == 0 && card != "" {
			if err = api.UpdateCard(ctx, card, c.JSON()); err == nil {
				mid = card
			} else {
				h.logf("bots: answer into the progress card: %v; sending it", err)
			}
		}
		if mid == "" {
			mid, err = h.botPost(ctx, api, "", r.MessageID, c, piece)
		}
		if err != nil {
			h.logf("bots: answer: %v", err)
			continue
		}
		h.rememberBotThread(mid, r.Target, r.Label)
	}
	if !held {
		for _, f := range append(folded, botFolded{MessageID: r.MessageID, Reaction: reaction}) {
			if f.Reaction != "" {
				_ = api.Unreact(ctx, f.MessageID, f.Reaction)
			}
			if _, err := api.React(ctx, f.MessageID, "DONE"); err != nil {
				h.logf("bots: react: %v", err)
			}
		}
	}
}

// botAnswered: the conversation's turn is (or just was) answered in Feishu, so the phone's "finished" push and the
// sync mirror leave it alone. "Just": within the finished push's delay (notify.go), which is when it looks — not much
// longer, or the push of the next turn, asked in the app, would be dropped too.
func (h *Hub) botAnswered(session string) bool {
	window := 30 * time.Second
	if h.cfg != nil {
		window += h.cfg.Notify.doneDelay()
	}
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.st.Runs {
		if r.Target.SessionID == session {
			return true
		}
	}
	at, ok := b.answered[session]
	return ok && b.clock().Sub(at) < window
}

// botTurnEnded: a conversation finished a turn (machine.go). Its runs are checked at once rather than on the next
// tick; a main conversation turn nobody asked for in Feishu is mirrored there when sync is on.
func (h *Hub) botTurnEnded(machine, session string) {
	b := &h.bots
	b.mu.Lock()
	var ids []string
	for _, r := range b.st.Runs {
		if r.Target.MachineID == machine && r.Target.SessionID == session {
			r.Seen = true
			if p := b.live[r.ID]; p != nil {
				p.checked = time.Time{} // a turn ended: look now, whatever the last look found
			}
			ids = append(ids, r.ID)
		}
	}
	b.mu.Unlock()
	for _, id := range ids {
		h.checkRun(id)
	}
	if len(ids) == 0 && !h.botAnswered(session) {
		h.botMirror(machine, session)
	}
}

// sweepBotRuns makes sure every run has its watcher (one lost to a restart of the bot).
func (h *Hub) sweepBotRuns(ctx context.Context) {
	b := &h.bots
	b.mu.Lock()
	var lost []*botRun
	if b.api != nil {
		for _, r := range b.st.Runs {
			if b.live[r.ID] == nil {
				lost = append(lost, r)
			}
		}
	}
	b.mu.Unlock()
	for _, r := range lost {
		go h.watchRun(r)
	}
}
