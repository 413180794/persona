package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
)

// The Feishu bot's turns go through the channels' pipeline (channels_runs.go); this is how they look in Feishu: a
// progress card replying to the message after a few seconds, rewritten every few, the answer written into it (or
// cards of their own when long), and the message's reaction going from OnIt to DONE.

const feishuChannelID = channel.Feishu

// botRun is a run as bots.feishu.state kept it before the channels' pipeline: read once and moved over
// (migrateBotRuns).
type botRun struct {
	ID        string      `json:"id"`
	MessageID string      `json:"message_id"`
	Target    botTarget   `json:"target"`
	Label     string      `json:"label"`
	Base      int         `json:"base"`
	StartedAt time.Time   `json:"started_at"`
	Card      string      `json:"card,omitempty"`
	Reaction  string      `json:"reaction,omitempty"`
	Seen      bool        `json:"seen,omitempty"`
	Held      bool        `json:"held,omitempty"`
	Probe     string      `json:"probe,omitempty"`
	Folded    []botFolded `json:"folded,omitempty"`
}

type botFolded struct {
	MessageID string `json:"message_id"`
	Reaction  string `json:"reaction,omitempty"`
}

// channelRun is a legacy run as the pipeline's.
func (r *botRun) channelRun() *channel.Run {
	out := &channel.Run{ID: r.ID, Channel: feishuChannelID, ReplyTo: r.MessageID, MachineID: r.Target.MachineID, SessionID: r.Target.SessionID,
		TargetKind: r.Target.Kind, Label: r.Label, Base: r.Base, Probe: r.Probe, Started: r.StartedAt, Seen: r.Seen, Held: r.Held,
		Ext: map[string]string{}}
	if r.MessageID != "" && !r.Held {
		out.MsgIDs = []string{channel.ClientMsgID(feishuChannelID, r.MessageID)}
	}
	if r.Card != "" {
		out.Ext["card"] = r.Card
	}
	if r.Reaction != "" {
		out.Ext["reaction"] = r.Reaction
	}
	for _, f := range r.Folded {
		fo := channel.Folded{ReplyTo: f.MessageID}
		if f.Reaction != "" {
			fo.Ext = map[string]string{"reaction": f.Reaction}
		}
		out.Folded = append(out.Folded, fo)
	}
	return out
}

// migrateBotRuns moves the runs bots.feishu.state still holds into the channels' state (an older hub's).
func (h *Hub) migrateBotRuns(ctx context.Context) {
	b := &h.bots
	b.mu.Lock()
	h.loadBotsLocked(ctx)
	old := b.st.Runs
	if len(old) > 0 {
		b.st.Runs = nil
		h.saveBotStateLocked()
	}
	b.mu.Unlock()
	if len(old) == 0 {
		return
	}
	c := &h.channels
	c.mu.Lock()
	h.loadChannelStateLocked(ctx)
	for _, r := range old {
		if r != nil && r.Target.SessionID != "" {
			c.st.Runs = append(c.st.Runs, r.channelRun())
		}
	}
	h.saveChannelStateLocked()
	c.mu.Unlock()
	h.logf("bots: %d waiting turns moved to the channels' pipeline", len(old))
}

// botTargetOf is what a run's conversation is to the chat: replies to the bot's messages about it go there.
func botTargetOf(r *channel.Run) botTarget {
	kind := r.TargetKind
	if kind == "" || kind == targetMain {
		kind = targetJarvis
	}
	return botTarget{Kind: kind, MachineID: r.MachineID, SessionID: r.SessionID}
}

// feishuSink is the Feishu bot as the pipeline's sink: cards.
type feishuSink struct{ h *Hub }

func (f feishuSink) progress(ctx context.Context, r *channel.Run, took time.Duration, steps []string, live string) {
	h := f.h
	api, _ := h.botAPI()
	if api == nil {
		return
	}
	c := progressCard(r.Label, took, steps, live)
	if card := r.Ext["card"]; card != "" {
		if err := api.UpdateCard(ctx, card, c.JSON()); err != nil {
			h.logf("bots: progress update: %v", err)
		}
		return
	}
	mid, err := api.Reply(ctx, r.ReplyTo, feishubot.TypeInteractive, c.JSON(), "", false)
	if err != nil {
		h.logf("bots: progress card: %v", err)
		return
	}
	h.setRunExt(r, "card", mid)
	h.rememberBotThread(mid, botTargetOf(r), r.Label)
}

func (f feishuSink) folded(ctx context.Context, o *channel.Run) {
	api, _ := f.h.botAPI()
	if api != nil && o.Ext["card"] != "" {
		_ = api.UpdateCard(ctx, o.Ext["card"], feishubot.NewCard("", "", "").Note("接着下一条消息一起回答").JSON())
	}
}

// answer writes the answer into the progress card (more cards when long), and marks the messages DONE.
func (f feishuSink) answer(ctx context.Context, r *channel.Run, answer, note string, took time.Duration, final, quiet bool) {
	h := f.h
	api, _ := h.botAPI()
	if api == nil {
		return
	}
	foot := r.Label + " · 用时 " + durationText(took)
	if note != "" {
		foot += "\n" + note
	}
	var pieces []string
	if !quiet {
		pieces = feishubot.Split(feishubot.CardMarkdown(answer), botAnswerPiece)
	}
	card := r.Ext["card"]
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
			mid, err = h.botPost(ctx, api, "", r.ReplyTo, c, piece)
		}
		if err != nil {
			h.logf("bots: answer: %v", err)
			continue
		}
		h.rememberBotThread(mid, botTargetOf(r), r.Label)
	}
	if !final {
		return
	}
	for _, m := range append(r.Folded, channel.Folded{ReplyTo: r.ReplyTo, Ext: r.Ext}) {
		if m.ReplyTo == "" {
			continue
		}
		if rx := m.Ext["reaction"]; rx != "" {
			_ = api.Unreact(ctx, m.ReplyTo, rx)
		}
		if _, err := api.React(ctx, m.ReplyTo, "DONE"); err != nil {
			h.logf("bots: react: %v", err)
		}
	}
}

const (
	botAnswerPiece = 6000    // runes per card: a card holds about 30 KB
	botImageMax    = 3 << 20 // a picture's base64 must stay under the model's 5 MB
	botFileMax     = 50 << 20
	botImagesMax   = 4
)

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

func durationText(d time.Duration) string { return channel.Duration(d) }

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
