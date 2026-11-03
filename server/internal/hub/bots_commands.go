package hub

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
)

// Commands in the bot's chat (docs/bots.md「命令」): which conversation or agent messages go to, the lists to pick one
// from, starting an agent, stopping a turn.

func botHelp(f BotFeatures) string {
	var b strings.Builder
	b.WriteString("直接发消息就是和 Persona 说话（文字、图片、文件、转发的聊天记录都行）；回复我的某条消息，就是接着那段对话说。\n\n")
	if f.Main {
		b.WriteString("- `/main` 回到主对话\n- `/new` 开一段新对话（下一条消息开始）\n- `/list` 最近的对话，点一下切换\n")
	}
	if f.Agents {
		b.WriteString("- `/agents` 各台电脑上的 Agent，点一下就在这里和它聊\n- `/start` 在某台电脑上开一个新 Agent\n")
	}
	b.WriteString("- `/stop` 停下当前这一轮\n- `/status` 现在发给谁\n- `/help` 这些说明")
	return b.String()
}

// botCommand runs a /command. Not handled, the message goes on with the text returned: as it is for a command it does
// not know (it may be the agent's own, like /compact), without the command for "/new <message>".
func (h *Hub) botCommand(ctx context.Context, api *feishubot.Client, set FeishuBotSettings, messageID, text string) (bool, string) {
	cmd, rest, _ := strings.Cut(strings.TrimSpace(text), " ")
	rest = strings.TrimSpace(rest)
	chat := set.Owner.ChatID
	reply := func(c *feishubot.Card) {
		if _, err := h.botPost(ctx, api, chat, messageID, c, ""); err != nil {
			h.logf("bots: command reply: %v", err)
		}
	}
	setCurrent := func(t botTarget) {
		h.bots.mu.Lock()
		h.bots.st.Current = t
		h.saveBotStateLocked()
		h.bots.mu.Unlock()
	}
	switch strings.ToLower(cmd) {
	case "/help", "/帮助":
		reply(feishubot.NewCard("怎么用", feishubot.Blue, "怎么用").Markdown(botHelp(set.Features)))
	case "/main", "/jarvis", "/主对话":
		if !set.Features.Main {
			h.botNote(ctx, api, messageID, "和 Persona 对话没有打开。")
			return true, ""
		}
		setCurrent(botTarget{Kind: targetMain})
		h.botNote(ctx, api, messageID, "好，现在发给主对话。")
	case "/new", "/新对话":
		if !set.Features.Main {
			h.botNote(ctx, api, messageID, "和 Persona 对话没有打开。")
			return true, ""
		}
		setCurrent(botTarget{Kind: targetJarvis})
		if rest == "" {
			h.botNote(ctx, api, messageID, "好，下一条消息开一段新对话。发 /main 回到主对话。")
			return true, ""
		}
		return false, rest // "/new 帮我…": the rest starts it
	case "/list", "/对话":
		if !set.Features.Main {
			h.botNote(ctx, api, messageID, "和 Persona 对话没有打开。")
			return true, ""
		}
		reply(h.conversationsCard(ctx))
	case "/agents", "/agent":
		if !set.Features.Agents {
			h.botNote(ctx, api, messageID, "在飞书里驱动 Agent 没有打开（App 设置 › 连接器 › 飞书机器人）。")
			return true, ""
		}
		reply(h.agentsCard(rest))
	case "/start", "/开工":
		if !set.Features.Agents {
			h.botNote(ctx, api, messageID, "在飞书里驱动 Agent 没有打开（App 设置 › 连接器 › 飞书机器人）。")
			return true, ""
		}
		reply(h.startAgentCard(rest))
	case "/stop", "/停":
		t := h.botTargetFor("", "")
		rt, err := h.resolveBotTarget(ctx, t)
		if err != nil || rt.isNew {
			h.botNote(ctx, api, messageID, "现在没有在跑的。")
			return true, ""
		}
		if err := rt.m.call(ctx, "session.interrupt", map[string]any{"session_id": rt.session.SessionID}, nil); err != nil {
			h.botNote(ctx, api, messageID, "没停下来："+clip(oneLine(err.Error()), 200))
			return true, ""
		}
		h.botNote(ctx, api, messageID, "停下了「"+rt.label+"」这一轮。")
	case "/status", "/状态":
		t := h.botTargetFor("", "")
		var b strings.Builder
		fmt.Fprintf(&b, "现在发给：**%s**\n", feishubot.EscapeMarkdown(h.targetLabel(ctx, t)))
		h.bots.mu.Lock()
		running := len(h.bots.st.Runs)
		h.bots.mu.Unlock()
		if running > 0 {
			fmt.Fprintf(&b, "在等 %d 个回答。\n", running)
		}
		reply(feishubot.NewCard("", "", "状态").Markdown(strings.TrimSpace(b.String())))
	default:
		return false, text
	}
	return true, ""
}

// conversationsCard lists recent Persona conversations with a button to switch to each.
func (h *Hub) conversationsCard(ctx context.Context) *feishubot.Card {
	v := h.assistantState(ctx, false)
	list := v.Conversations
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	c := feishubot.NewCard("最近的对话", feishubot.Blue, "最近的对话")
	if len(list) == 0 {
		return c.Markdown("还没有对话。直接发消息就会开始主对话。")
	}
	for i, s := range list {
		if i == 8 {
			break
		}
		name := conversationName(s, v.Main)
		line := "**" + feishubot.EscapeMarkdown(name) + "**"
		if !s.UpdatedAt.IsZero() {
			line += " · " + ago(s.UpdatedAt)
		}
		if s.LastText != "" {
			line += "\n" + feishubot.EscapeMarkdown(clip(oneLine(s.LastText), 80))
		}
		kind := targetJarvis
		if s.SessionID == v.Main {
			kind = targetMain
		}
		c.Markdown(line).Buttons(feishubot.Btn("在这里聊", feishubot.Default, map[string]any{"a": "switch", "k": kind, "m": s.MachineID, "s": s.SessionID, "t": name}))
	}
	return c.Note("发 /new 开一段新对话")
}

// agentsCard lists the agents worth talking to: needing you, working, finished unseen, then the ones idle lately.
func (h *Hub) agentsCard(query string) *feishubot.Card {
	type row struct {
		m *Machine
		s proto.Session
	}
	var rows []row
	q := strings.ToLower(query)
	for _, m := range h.machinesNow() {
		for _, s := range h.decorated(m) {
			if s.Assistant || s.State == proto.StateOffline || s.State == proto.StateUnknown {
				continue
			}
			if s.State == proto.StateIdle && time.Since(s.UpdatedAt) > 48*time.Hour {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(s.Title+" "+s.Cwd+" "+s.SpaceName+" "+s.SlotName+" "+m.cfg.Name), q) {
				continue
			}
			rows = append(rows, row{m, s})
		}
	}
	order := map[string]int{proto.StateNeedsYou: 0, proto.StateWorking: 1, proto.StateUnread: 2, proto.StateIdle: 3}
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oj := order[rows[i].s.State], order[rows[j].s.State]
		if oi != oj {
			return oi < oj
		}
		return rows[i].s.UpdatedAt.After(rows[j].s.UpdatedAt)
	})
	c := feishubot.NewCard("各台电脑上的 Agent", feishubot.Purple, "Agent 列表")
	if len(rows) == 0 {
		return c.Markdown("没有在线的 Agent。发 /start 开一个。")
	}
	icons := map[string]string{proto.StateNeedsYou: "🔴", proto.StateWorking: "🟢", proto.StateUnread: "🔵", proto.StateIdle: "⚪️"}
	for i, r := range rows {
		if i == 10 {
			c.Note(fmt.Sprintf("还有 %d 个，发 /agents <关键词> 筛选", len(rows)-10))
			break
		}
		label := agentLabel(r.s)
		line := icons[r.s.State] + " **" + feishubot.EscapeMarkdown(label) + "** · " + feishubot.EscapeMarkdown(r.m.cfg.Name) + " · " + stateNames[r.s.State]
		if r.s.LastText != "" {
			line += "\n" + feishubot.EscapeMarkdown(clip(oneLine(r.s.LastText), 90))
		}
		c.Markdown(line).Buttons(feishubot.Btn("在这里聊", feishubot.Default,
			map[string]any{"a": "switch", "k": targetAgent, "m": r.m.cfg.ID, "s": r.s.SessionID, "t": label + " · " + r.m.cfg.Name}))
	}
	return c
}

// startAgentCard is a form to start a coding agent on a machine.
func (h *Hub) startAgentCard(prompt string) *feishubot.Card {
	var opts []feishubot.Option
	for _, m := range h.machinesNow() {
		if m.snapshot().Online {
			opts = append(opts, feishubot.Option{Label: m.cfg.Name, Value: m.cfg.ID})
		}
	}
	c := feishubot.NewCard("开一个新 Agent", feishubot.Purple, "开一个新 Agent")
	if len(opts) == 0 {
		return c.Markdown("没有在线的电脑。")
	}
	fields := []any{
		feishubot.Select("machine", "哪台电脑", opts, false),
		feishubot.Input("cwd", "目录", "~/code/项目", false),
		feishubot.Input("prompt", "要它做什么", "可以留空，之后在这里接着说", true),
	}
	if prompt != "" {
		fields[2].(map[string]any)["default_value"] = prompt
	}
	return c.Form("start", fields, feishubot.Submit("开始", feishubot.Primary, "start_submit", map[string]any{"a": "start"}))
}
