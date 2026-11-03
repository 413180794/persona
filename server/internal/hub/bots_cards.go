package hub

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/feishubot"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Cards for what Jarvis and the agents ask (bots.go BotFeatures.Cards): a question, a plan, a permission or a terminal
// dialog shows in the chat as a card with the same choices as the app's card; answering it in either place answers it
// (h.resolve), and the card in Feishu then says how it was answered. Cards go out for what the chat drives (its target,
// and the conversations of its runs), or for everything with AllCards.

// botInteraction: a new interaction (hub.go announce).
func (h *Hub) botInteraction(ix proto.Interaction) {
	api, set := h.botAPI()
	if api == nil || !set.Features.Cards {
		return
	}
	if !set.Features.AllCards && !h.botDrives(ix.MachineID, ix.SessionID) {
		return
	}
	b := &h.bots
	b.mu.Lock()
	if _, ok := b.cards[ix.ID]; ok {
		b.mu.Unlock()
		return
	}
	if b.cards == nil {
		b.cards = map[string]botCard{}
	}
	b.cards[ix.ID] = botCard{} // claimed: a second announce of the same card does not post twice
	reply := ""
	for _, r := range b.st.Runs {
		if r.Target.MachineID == ix.MachineID && r.Target.SessionID == ix.SessionID {
			reply = r.MessageID
		}
	}
	b.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := h.interactionCard(ix)
		mid, err := h.botPost(ctx, api, set.Owner.ChatID, reply, c, "")
		b.mu.Lock()
		defer b.mu.Unlock()
		if err != nil {
			h.logf("bots: card for %s: %v", ix.ID, err)
			delete(b.cards, ix.ID)
			return
		}
		if _, still := b.cards[ix.ID]; !still { // answered while it was being posted
			go h.botCardDone(api, mid, ix, "已处理")
			return
		}
		b.cards[ix.ID] = botCard{MessageID: mid}
	}()
}

// botDrives: the chat drives this conversation (its target now, a run on it, or a thread bound to it).
func (h *Hub) botDrives(machine, session string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b := &h.bots
	b.mu.Lock()
	cur := b.st.Current
	for _, r := range b.st.Runs {
		if r.Target.MachineID == machine && r.Target.SessionID == session {
			b.mu.Unlock()
			return true
		}
		// The hub's own approvals (mail_send) do not name the conversation: one on the assistant's machine while a
		// Jarvis run goes on is most likely that run's.
		if session == "" && !r.Target.agent() && r.Target.MachineID == machine {
			b.mu.Unlock()
			return true
		}
	}
	for _, t := range b.st.Threads {
		if t.Target.MachineID == machine && t.Target.SessionID == session && b.clock().Sub(t.At) < 24*time.Hour {
			b.mu.Unlock()
			return true
		}
	}
	b.mu.Unlock()
	if session == "" {
		return false
	}
	if cur.Kind == targetMain {
		cfg := h.assistantConfig(ctx)
		return cfg.MachineID == machine && cfg.Main == session
	}
	return cur.MachineID == machine && cur.SessionID == session
}

// botInteractionDone: an interaction is over (answered anywhere, withdrawn, expired): its card says so.
func (h *Hub) botInteractionDone(ix proto.Interaction, how string) {
	b := &h.bots
	b.mu.Lock()
	c, ok := b.cards[ix.ID]
	delete(b.cards, ix.ID)
	b.mu.Unlock()
	if !ok || c.MessageID == "" {
		return
	}
	api, _ := h.botAPI()
	if api == nil {
		return
	}
	go h.botCardDone(api, c.MessageID, ix, how)
}

func (h *Hub) botCardDone(api *feishubot.Client, messageID string, ix proto.Interaction, how string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := api.UpdateCard(ctx, messageID, h.answeredCard(ix, how).JSON()); err != nil {
		h.logf("bots: close card %s: %v", ix.ID, err)
	}
}

// resolutionText says how an interaction was answered, for its closed card.
func resolutionText(ix proto.Interaction, r proto.Resolution) string {
	switch {
	case r.Dismiss:
		return "已忽略"
	case r.Approve != nil && *r.Approve:
		switch r.Mode {
		case "bypassPermissions":
			return "已批准，自动执行"
		case "acceptEdits":
			return "已批准，自动接受编辑"
		}
		return "已批准"
	case r.Approve != nil:
		if r.Feedback != "" {
			return "已打回：" + clip(r.Feedback, 200)
		}
		return "已打回"
	case r.Allow != nil && *r.Allow:
		if r.Always {
			return "已允许（本会话不再问）"
		}
		return "已允许"
	case r.Allow != nil:
		if r.Message != "" {
			return "已拒绝：" + clip(r.Message, 200)
		}
		return "已拒绝"
	case len(r.Answers) > 0:
		var parts []string
		for _, q := range ix.Questions {
			if a, ok := r.Answers[q.Question]; ok {
				parts = append(parts, a)
			}
		}
		if len(parts) == 0 {
			for _, a := range r.Answers {
				parts = append(parts, a)
			}
		}
		return "已回答：" + clip(strings.Join(parts, "；"), 300)
	case r.Option != "" && ix.Dialog != nil:
		if o := ix.Dialog.Option(r.Option); o != nil {
			return "已选：" + o.Label
		}
	case len(r.Keys) > 0 || r.Text != "":
		return "已作答"
	}
	return "已处理"
}

func (h *Hub) ixWhere(ix proto.Interaction) string {
	where := ix.SessionTitle
	if where == "" {
		where = "Jarvis"
	}
	if m := h.localMachine(ix.MachineID); m != nil && where != "Jarvis" {
		where += " · " + m.cfg.Name
	}
	return where
}

// interactionCard is the card asking ix, with the app's choices.
func (h *Hub) interactionCard(ix proto.Interaction) *feishubot.Card {
	where := feishubot.EscapeMarkdown(h.ixWhere(ix))
	val := func(extra map[string]any) map[string]any {
		v := map[string]any{"a": "ix", "id": ix.ID}
		for k, x := range extra {
			v[k] = x
		}
		return v
	}
	open := h.openButton(ix.MachineID, ix.SessionID)
	switch ix.Kind {
	case proto.IxQuestion:
		c := feishubot.NewCard("❓ "+h.ixWhere(ix)+" 在问你", feishubot.Orange, "在问你："+firstQuestion(ix))
		if len(ix.Questions) == 1 && !ix.Questions[0].MultiSelect && len(ix.Questions[0].Options) <= 4 {
			q := ix.Questions[0]
			c.Markdown(questionText(q))
			var bs []feishubot.Button
			for _, o := range q.Options {
				bs = append(bs, feishubot.Btn(clip(o.Label, 30), feishubot.Default, val(map[string]any{"q": "0", "v": o.Label})))
			}
			c.Buttons(bs...)
			c.Form("other", []any{feishubot.Input("text", "", "或者直接写你的回答", false)},
				feishubot.Submit("回答", feishubot.Primary, "q_submit", val(map[string]any{"q": "0", "free": "1"})))
		} else {
			var fields []any
			for i, q := range ix.Questions {
				fields = append(fields, feishubot.MarkdownBlock(questionText(q)))
				var opts []feishubot.Option
				for _, o := range q.Options {
					opts = append(opts, feishubot.Option{Label: clip(o.Label, 60), Value: o.Label})
				}
				if len(opts) > 0 {
					fields = append(fields, feishubot.Select("q"+strconv.Itoa(i), "选一个"+map[bool]string{true: "或几个"}[q.MultiSelect], opts, q.MultiSelect))
				}
				fields = append(fields, feishubot.Input("t"+strconv.Itoa(i), "", "或者直接写", false))
			}
			c.Form("answers", fields, feishubot.Submit("提交", feishubot.Primary, "qs_submit", val(map[string]any{"form": "1"})))
		}
		return c.Buttons(open, feishubot.Btn("忽略", feishubot.Default, val(map[string]any{"dismiss": "1"})))
	case proto.IxPlan:
		c := feishubot.NewCard("📋 "+h.ixWhere(ix)+" 的计划等你批准", feishubot.Blue, "计划等你批准")
		plan := ix.Plan
		if r := []rune(plan); len(r) > 5000 {
			plan = string(r[:5000]) + "\n\n…（太长，全文到 App 里看）"
		}
		c.Markdown(feishubot.CardMarkdown(plan))
		c.Buttons(
			feishubot.Btn("批准并自动执行", feishubot.Primary, val(map[string]any{"approve": "1", "mode": "bypassPermissions"})),
			feishubot.Btn("自动接受编辑", feishubot.Default, val(map[string]any{"approve": "1", "mode": "acceptEdits"})),
		)
		c.Form("reject", []any{feishubot.Input("feedback", "", "要怎么改（打回时写）", true)},
			feishubot.Submit("打回修改", feishubot.Danger, "plan_reject", val(map[string]any{"approve": "0"})))
		return c.Buttons(open)
	case proto.IxPermission:
		c := feishubot.NewCard("🔐 "+h.ixWhere(ix)+" 要你批准", feishubot.Orange, "要你批准："+ix.ToolName)
		var b strings.Builder
		fmt.Fprintf(&b, "**%s**", feishubot.EscapeMarkdown(ix.ToolName))
		if ix.Reason != "" {
			fmt.Fprintf(&b, "\n%s", feishubot.EscapeMarkdown(ix.Reason))
		}
		c.Markdown(b.String())
		if s := strings.TrimSpace(ix.Summary); s != "" {
			c.Markdown("```\n" + strings.ReplaceAll(clip(s, 2500), "```", "'''") + "\n```")
		} else if len(ix.ToolInput) > 0 {
			c.Markdown("```json\n" + strings.ReplaceAll(clip(string(ix.ToolInput), 2500), "```", "'''") + "\n```")
		}
		if len(ix.Fields) > 0 {
			// A password or a code is typed in the app, never in a chat.
			c.Note("这一步要在 App 里填写内容，飞书里不能批准。")
			return c.Buttons(open)
		}
		bs := []feishubot.Button{
			feishubot.Btn("允许", feishubot.Primary, val(map[string]any{"allow": "1"})),
			feishubot.Btn("拒绝", feishubot.Danger, val(map[string]any{"allow": "0"})),
		}
		if ix.AlwaysLabel != "" || ix.SessionID != "" { // as the app's card
			label := ix.AlwaysLabel
			if label == "" {
				label = "本会话总是允许"
			}
			bs = append(bs, feishubot.Btn(clip(label, 20), feishubot.Default, val(map[string]any{"allow": "1", "always": "1"})))
		}
		return c.Buttons(bs...).Buttons(open)
	case proto.IxScreen:
		c := feishubot.NewCard("🖥 "+h.ixWhere(ix)+" 在终端里等你", feishubot.Orange, "终端里等你")
		if d := ix.Dialog; d != nil {
			q := d.Question
			if d.Detail != "" {
				q = d.Detail + "\n" + q
			}
			c.Markdown(feishubot.EscapeMarkdown(q))
			var bs []feishubot.Button
			for _, o := range d.Options {
				if o.Kind == "type_something" || o.Kind == "chat" {
					continue
				}
				bs = append(bs, feishubot.Btn(clip(o.Label, 30), feishubot.Default, val(map[string]any{"opt": o.Key})))
			}
			c.Buttons(bs...)
		} else {
			screen := ix.Screen
			if lines := strings.Split(screen, "\n"); len(lines) > 20 {
				screen = strings.Join(lines[len(lines)-20:], "\n")
			}
			c.Markdown("```\n" + strings.ReplaceAll(screen, "```", "'''") + "\n```")
			c.Buttons(
				feishubot.Btn("回车", feishubot.Default, val(map[string]any{"keys": "enter"})),
				feishubot.Btn("Esc", feishubot.Default, val(map[string]any{"keys": "esc"})),
				feishubot.Btn("1", feishubot.Default, val(map[string]any{"keys": "1"})),
				feishubot.Btn("2", feishubot.Default, val(map[string]any{"keys": "2"})),
			)
		}
		if !ix.Secret {
			c.Form("type", []any{feishubot.Input("text", "", "输入文字后回车", false)},
				feishubot.Submit("发送", feishubot.Primary, "screen_text", val(map[string]any{"typed": "1"})))
		} else {
			c.Note("这里要输入密码：到 App 里输入。")
		}
		return c.Buttons(open)
	}
	return feishubot.NewCard(where, feishubot.Grey, "").Buttons(open)
}

func firstQuestion(ix proto.Interaction) string {
	if len(ix.Questions) > 0 {
		return ix.Questions[0].Question
	}
	return ""
}

func questionText(q proto.Question) string {
	s := ""
	if q.Header != "" {
		s = "**" + feishubot.EscapeMarkdown(q.Header) + "**\n"
	}
	s += feishubot.EscapeMarkdown(q.Question)
	for _, o := range q.Options {
		if o.Description != "" {
			s += "\n- " + feishubot.EscapeMarkdown(o.Label) + "：" + feishubot.EscapeMarkdown(o.Description)
		}
	}
	return s
}

// answeredCard is an interaction's card once it is over.
func (h *Hub) answeredCard(ix proto.Interaction, how string) *feishubot.Card {
	title := map[string]string{proto.IxQuestion: "❓ ", proto.IxPlan: "📋 ", proto.IxPermission: "🔐 ", proto.IxScreen: "🖥 "}[ix.Kind] + h.ixWhere(ix)
	c := feishubot.NewCard(title, feishubot.Grey, how)
	switch ix.Kind {
	case proto.IxQuestion:
		for _, q := range ix.Questions {
			c.Markdown(feishubot.EscapeMarkdown(q.Question))
		}
	case proto.IxPermission:
		c.Markdown(feishubot.EscapeMarkdown(ix.ToolName + " " + clip(oneLine(ix.Summary), 200)))
	case proto.IxPlan:
		c.Markdown(feishubot.EscapeMarkdown(clip(firstLine(ix.Plan), 200)))
	}
	return c.Note("✓ " + how)
}

// openButton opens the conversation in the web app the hub serves. Card links are documented for http(s) and AppLink
// only: a jarvis:// link (the iPhone app) might get the whole card refused, so it is not used.
func (h *Hub) openButton(machine, session string) feishubot.Button {
	web := strings.TrimRight(h.cfg.PublicURL, "/")
	if session != "" {
		// A Jarvis conversation has its own page (/j/<id>, web/src/lib/router.ts); an agent is /s/<machine>/<id>.
		s := proto.Session{}
		m := h.localMachine(machine)
		if m != nil {
			s, _ = m.session(session)
		}
		if m != nil && isAssistant(s, m.assistantCwd()) {
			web += "/j/" + url.PathEscape(session)
		} else {
			web += "/s/" + url.PathEscape(machine) + "/" + url.PathEscape(session)
		}
	}
	return feishubot.LinkBtn("打开网页版", web)
}

// --- card buttons ---

// botCardAction answers a press on one of the bot's cards. Only the owner's presses count.
func (h *Hub) botCardAction(ctx context.Context, a *feishubot.CardAction) *feishubot.CardResponse {
	_, set := h.botAPI()
	if set.Owner == nil || a.Operator.OpenID != set.Owner.OpenID {
		return toast("error", "只有机器人的主人能操作")
	}
	switch a.Str("a") {
	case "ix":
		return h.answerFromCard(a)
	case "switch":
		kind, m, s, title := a.Str("k"), a.Str("m"), a.Str("s"), a.Str("t")
		t := botTarget{Kind: kind, MachineID: m, SessionID: s}
		if kind == targetMain {
			t = botTarget{Kind: targetMain}
		}
		if err := h.botAllowed(set.Features, t); err != nil {
			return toast("error", err.Error())
		}
		h.bots.mu.Lock()
		h.bots.st.Current = t
		h.saveBotStateLocked()
		h.bots.mu.Unlock()
		return toast("success", "现在发给「"+clip(title, 30)+"」")
	case "start":
		if !set.Features.Agents {
			return toast("error", "在飞书里驱动 Agent 没有打开")
		}
		machine, cwd, prompt := a.Form("machine"), strings.TrimSpace(a.Form("cwd")), strings.TrimSpace(a.Form("prompt"))
		if machine == "" || cwd == "" {
			return toast("warning", "选好电脑、填上目录")
		}
		go h.botStartAgent(machine, cwd, prompt, a.Context.MessageID)
		return toast("info", "正在开…")
	}
	return toast("info", "这张卡片已经过期了")
}

func toast(kind, text string) *feishubot.CardResponse {
	return &feishubot.CardResponse{Toast: &feishubot.Toast{Type: kind, Content: text}}
}

// answerFromCard turns a press on an interaction card into the same answer the app would give, and resolves it.
func (h *Hub) answerFromCard(a *feishubot.CardAction) *feishubot.CardResponse {
	id := a.Str("id")
	h.mu.Lock()
	p := h.interactions[id]
	h.mu.Unlock()
	if p == nil {
		return toast("info", "已经处理过了")
	}
	ix := p.ix
	yes, no := true, false
	var r proto.Resolution
	switch {
	case a.Str("dismiss") == "1":
		r.Dismiss = true
	case ix.Kind == proto.IxQuestion && a.Str("form") == "1":
		r.Answers = map[string]string{}
		for i, q := range ix.Questions {
			ans := strings.TrimSpace(a.Form("t" + strconv.Itoa(i)))
			if ans == "" {
				ans = a.Form("q" + strconv.Itoa(i))
			}
			if ans != "" {
				r.Answers[q.Question] = ans
			}
		}
		if len(r.Answers) < len(ix.Questions) {
			return toast("warning", "每个问题都要回答")
		}
	case ix.Kind == proto.IxQuestion:
		i, _ := strconv.Atoi(a.Str("q"))
		if i < 0 || i >= len(ix.Questions) {
			return toast("error", "问题对不上了")
		}
		ans := a.Str("v")
		if a.Str("free") == "1" {
			ans = strings.TrimSpace(a.Form("text"))
		}
		if ans == "" {
			return toast("warning", "写上回答")
		}
		r.Answers = map[string]string{ix.Questions[i].Question: ans}
	case ix.Kind == proto.IxPlan && a.Str("approve") == "1":
		r.Approve, r.Mode = &yes, a.Str("mode")
	case ix.Kind == proto.IxPlan:
		r.Approve, r.Feedback = &no, strings.TrimSpace(a.Form("feedback"))
	case ix.Kind == proto.IxPermission:
		if len(ix.Fields) > 0 {
			return toast("warning", "这一步要在 App 里填写")
		}
		if a.Str("allow") == "1" {
			r.Allow, r.Always = &yes, a.Str("always") == "1"
		} else {
			r.Allow = &no
		}
	case ix.Kind == proto.IxScreen && a.Str("opt") != "":
		r.Option = a.Str("opt")
	case ix.Kind == proto.IxScreen && a.Str("typed") == "1":
		text := a.Form("text")
		if text == "" {
			return toast("warning", "先写上要输入的")
		}
		r.Keys, r.Text = []string{"enter"}, text
	case ix.Kind == proto.IxScreen && a.Str("keys") != "":
		r.Keys = []string{a.Str("keys")}
	default:
		return toast("error", "看不懂这个操作")
	}
	// This card is closed by the answer below, not again when the interaction ends.
	b := &h.bots
	b.mu.Lock()
	card, had := b.cards[id]
	delete(b.cards, id)
	b.mu.Unlock()
	restore := func() {
		if had {
			b.mu.Lock()
			b.cards[id] = card
			b.mu.Unlock()
		}
	}
	how := resolutionText(ix, r)
	// Screen and Codex cards act on the terminal and can take a few seconds; Feishu waits 3 s for this answer.
	done := make(chan error, 1)
	go func() { done <- h.resolve(id, r) }()
	select {
	case err := <-done:
		if err != nil {
			restore()
			return toast("error", clip(oneLine(err.Error()), 80))
		}
		return &feishubot.CardResponse{Toast: &feishubot.Toast{Type: "success", Content: how}, Card: h.answeredCard(ix, how).Raw()}
	case <-time.After(2 * time.Second):
		msg := a.Context.MessageID
		go func() {
			if err := <-done; err != nil {
				h.logf("bots: answer %s: %v", id, err)
				restore()
				return
			}
			if api, _ := h.botAPI(); api != nil && msg != "" {
				h.botCardDone(api, msg, ix, how)
			}
		}()
		return toast("info", "正在处理…")
	}
}

// botStartAgent starts a coding agent from the /start card and points the chat at it.
func (h *Hub) botStartAgent(machine, cwd, prompt, cardID string) {
	api, set := h.botAPI()
	if api == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m := h.localMachine(machine)
	say := func(text string) {
		if _, err := api.Send(ctx, "chat_id", set.Owner.ChatID, feishubot.TypeText, feishubot.TextContent(text), ""); err != nil {
			h.logf("bots: start agent note: %v", err)
		}
	}
	if m == nil {
		say("没有这台电脑。")
		return
	}
	if strings.HasPrefix(cwd, "~/") {
		if home := m.snapshot().Home; home != "" {
			cwd = path0(home, cwd[2:])
		}
	}
	params := map[string]any{"cwd": cwd, "prompt": prompt, "tab_name": h.autoTabName(ctx, prompt)}
	if err := h.applyModel(ctx, "agent.new", params); err != nil {
		say("开不了：" + err.Error())
		return
	}
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := m.call(ctx, "session.start", params, &res); err != nil {
		say("开不了：" + clip(oneLine(err.Error()), 200))
		return
	}
	t := botTarget{Kind: targetAgent, MachineID: m.cfg.ID, SessionID: res.Session.SessionID}
	label := agentLabel(res.Session)
	if label == "" || label == "." {
		label = cwd
	}
	label += " · " + m.cfg.Name
	h.bots.mu.Lock()
	h.bots.st.Current = t
	h.saveBotStateLocked()
	h.bots.mu.Unlock()
	c := feishubot.NewCard("", "", "已开始").Markdown("在 **" + feishubot.EscapeMarkdown(m.cfg.Name) + "** 的 " + feishubot.Code(cwd) + " 开了一个 Agent，现在发给它。")
	if prompt != "" {
		c.Note("它已经在做了，做完把结果发到这里。")
	}
	mid, err := h.botPost(ctx, api, set.Owner.ChatID, "", c.Buttons(h.openButton(m.cfg.ID, res.Session.SessionID)), "")
	if err != nil {
		h.logf("bots: started note: %v", err)
	}
	h.rememberBotThread(mid, t, label)
	if prompt != "" && mid != "" {
		h.startRun(&botRun{ID: "fb_" + mid, MessageID: mid, Target: t, Label: label, StartedAt: time.Now().UTC()})
	}
}

func path0(home, rel string) string { return strings.TrimRight(home, "/") + "/" + rel }

// --- pushes and the sync mirror ---

// botPush sends what notify_user pushed to the phone to Feishu too (BotFeatures.Pushes).
func (h *Hub) botPush(machine, session, title, body string) {
	api, set := h.botAPI()
	if api == nil || !set.Features.Pushes {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := feishubot.NewCard("🔔 "+clip(oneLine(title), 60), feishubot.Wathet, title).Markdown(feishubot.CardMarkdown(body))
		if session != "" {
			c.Buttons(h.openButton(machine, session))
		}
		mid, err := h.botPost(ctx, api, set.Owner.ChatID, "", c, body)
		if err != nil {
			h.logf("bots: push: %v", err)
			return
		}
		if session != "" {
			h.rememberBotThread(mid, botTarget{Kind: targetJarvis, MachineID: machine, SessionID: session}, title)
		}
	}()
}

// botMirror sends a turn that ended without being asked for in Feishu: the main conversation's when Sync is on (what
// was said in the app, a scheduled task's run), a scheduled task's own conversation's when Pushes is on.
func (h *Hub) botMirror(machine, session string) {
	api, set := h.botAPI()
	if api == nil || !(set.Features.Sync || set.Features.Pushes) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := h.assistantConfig(ctx)
	main := cfg.Main != "" && cfg.Main == session && cfg.MachineID == machine
	task := h.taskOfConversation(ctx, machine, session)
	switch {
	case main && set.Features.Sync:
	case task != "" && set.Features.Pushes:
	default:
		return
	}
	h.mu.Lock()
	pushed := time.Since(h.notified[session]) < 3*time.Minute
	h.mu.Unlock()
	if pushed && set.Features.Pushes {
		return // its notify_user already went to Feishu
	}
	m := h.localMachine(machine)
	if m == nil {
		return
	}
	s, ok := m.session(session)
	if !ok || s.WokeByTask && !main {
		return
	}
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := m.call(ctx, "session.items", map[string]any{"session_id": session, "turns": 1, "lazy_images": true}, &res); err != nil {
		return
	}
	asked := ""
	for _, it := range res.Items {
		if it.Role == "user" && it.Kind == proto.KindText && asked == "" {
			asked = strings.TrimSpace(it.Text)
		}
	}
	if strings.Contains(asked, "title=\"飞书机器人\"") {
		return // its own message: answered by the run
	}
	parts := turnAnswer(res.Items)
	if len(parts) == 0 {
		return
	}
	var texts []string
	for _, p := range parts {
		texts = append(texts, p.Text)
	}
	answer := strings.Join(texts, "\n\n")
	label := "主对话"
	if !main {
		label = "定时任务「" + task + "」"
	}
	c := feishubot.NewCard("", "", answer)
	if asked != "" && main {
		c.Note("在别处问：" + clip(oneLine(stripContext(asked)), 80))
	}
	pieces := feishubot.Split(feishubot.CardMarkdown(answer), botAnswerPiece)
	c.Markdown(pieces[0])
	if len(pieces) > 1 {
		c.Note("（只发了开头，全文到 App 里看）")
	}
	c.Note(label).Buttons(h.openButton(machine, session))
	mid, err := h.botPost(ctx, api, set.Owner.ChatID, "", c, pieces[0])
	if err != nil {
		h.logf("bots: mirror: %v", err)
		return
	}
	h.rememberBotThread(mid, botTarget{Kind: targetJarvis, MachineID: machine, SessionID: session}, label)
}

// taskOfConversation is the name of the scheduled task whose own conversation this is, or "".
func (h *Hub) taskOfConversation(ctx context.Context, machine, session string) string {
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.RunSessionID == session && t.RunMachineID == machine {
			return t.label()
		}
	}
	return ""
}

// stripContext leaves out the <jarvis-context> blocks of a message (what the user wrote is after them).
func stripContext(s string) string {
	for {
		i := strings.Index(s, "<jarvis-context")
		if i < 0 {
			return strings.TrimSpace(s)
		}
		j := strings.Index(s[i:], "</jarvis-context>")
		if j < 0 {
			return strings.TrimSpace(s[:i])
		}
		s = s[:i] + s[i+j+len("</jarvis-context>"):]
	}
}

// botResolution: an interaction was answered (hub.go resolve): its card says how.
func (h *Hub) botResolution(ix proto.Interaction, r proto.Resolution) {
	h.botInteractionDone(ix, resolutionText(ix, r))
}
