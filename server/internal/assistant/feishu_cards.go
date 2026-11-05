package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
)

// Cards for what Persona and the agents ask (the bot's cards switch): a question, a plan, a permission or a terminal
// dialog shows in the chat as a card with the same choices as the app's card; answering it in either place answers it
// (interactions.resolve), and the card in Feishu then says how it was answered (interaction.ended). Cards go out for
// what the chat drives (its target, and the conversations of its runs), or for everything with all_cards. And the
// chat's commands, /start, the pushes and the sync mirror.

// feishuInteraction: a new card (interaction.announced).
func (s *Service) feishuInteraction(ix proto.Interaction) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f, ready := s.feishuFeatures(ctx)
	if !ready || !f.Cards {
		return
	}
	if !f.AllCards && !s.botDrives(ctx, ix.MachineID, ix.SessionID) {
		return
	}
	if s.interactionEnded(ix.ID) { // ended while this looked at the bot: not asked (s.chans before s.fbot, never in it)
		return
	}
	b := &s.fbot
	b.mu.Lock()
	if _, ok := b.cards[ix.ID]; ok {
		b.mu.Unlock()
		return
	}
	if b.cards == nil {
		b.cards = map[string]botCard{}
	}
	b.cards[ix.ID] = botCard{} // claimed: a second announce of the same card does not post twice
	b.mu.Unlock()
	reply := ""
	for _, r := range s.channelRuns(channel.Feishu) {
		if r.MachineID == ix.MachineID && r.SessionID == ix.SessionID {
			reply = r.ReplyTo
		}
	}
	mid, err := s.botPost(ctx, reply, s.interactionCard(ctx, ix), "")
	ended := s.interactionEnded(ix.ID)
	b.mu.Lock()
	if err != nil {
		s.logf("bots: card for %s: %v", ix.ID, err)
		delete(b.cards, ix.ID)
		b.mu.Unlock()
		return
	}
	if _, still := b.cards[ix.ID]; !still || ended { // answered while it was being posted
		delete(b.cards, ix.ID)
		b.mu.Unlock()
		s.botCardDone(ctx, mid, ix, "已处理")
		return
	}
	b.cards[ix.ID] = botCard{MessageID: mid, Ix: ix}
	b.mu.Unlock()
}

// botDrives: the chat drives this conversation (its target now, a run on it, or a thread bound to it).
func (s *Service) botDrives(ctx context.Context, machine, session string) bool {
	for _, r := range s.channelRuns(channel.Feishu) {
		if r.MachineID == machine && r.SessionID == session {
			return true
		}
		// The hub's own approvals (mail_send) do not name the conversation: one on the assistant's machine while a
		// Persona run goes on is most likely that run's.
		if session == "" && r.TargetKind != targetAgent && r.MachineID == machine {
			return true
		}
	}
	b := &s.fbot
	b.mu.Lock()
	s.loadBotLocked(ctx)
	cur := b.st.Current
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
		return s.isMainOn(ctx, machine, session)
	}
	return cur.MachineID == machine && cur.SessionID == session
}

// feishuInteractionDone: a card is over (answered anywhere, withdrawn, expired): its card in Feishu says so.
func (s *Service) feishuInteractionDone(ix proto.Interaction, how string) {
	b := &s.fbot
	b.mu.Lock()
	c, ok := b.cards[ix.ID]
	delete(b.cards, ix.ID)
	b.mu.Unlock()
	if !ok || c.MessageID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.botCardDone(ctx, c.MessageID, ix, how)
	}()
}

func (s *Service) botCardDone(ctx context.Context, messageID string, ix proto.Interaction, how string) {
	if err := s.botUpdateCard(ctx, messageID, s.answeredCard(ctx, ix, how)); err != nil {
		s.logf("bots: close card %s: %v", ix.ID, err)
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

// interactionCard is the card asking ix, with the app's choices.
func (s *Service) interactionCard(ctx context.Context, ix proto.Interaction) *feishubot.Card {
	where := feishubot.EscapeMarkdown(s.ixWhere(ctx, ix))
	val := func(extra map[string]any) map[string]any {
		v := map[string]any{"a": "ix", "id": ix.ID}
		for k, x := range extra {
			v[k] = x
		}
		return v
	}
	open := s.openButton(ctx, ix.MachineID, ix.SessionID)
	switch ix.Kind {
	case proto.IxQuestion:
		c := feishubot.NewCard("❓ "+s.ixWhere(ctx, ix)+" 在问你", feishubot.Orange, "在问你："+firstQuestion(ix))
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
		c := feishubot.NewCard("📋 "+s.ixWhere(ctx, ix)+" 的计划等你批准", feishubot.Blue, "计划等你批准")
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
		c := feishubot.NewCard("🔐 "+s.ixWhere(ctx, ix)+" 要你批准", feishubot.Orange, "要你批准："+ix.ToolName)
		var b strings.Builder
		fmt.Fprintf(&b, "**%s**", feishubot.EscapeMarkdown(ix.ToolName))
		if ix.Reason != "" {
			fmt.Fprintf(&b, "\n%s", feishubot.EscapeMarkdown(ix.Reason))
		}
		c.Markdown(b.String())
		if sum := strings.TrimSpace(ix.Summary); sum != "" {
			c.Markdown("```\n" + strings.ReplaceAll(clip(sum, 2500), "```", "'''") + "\n```")
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
		c := feishubot.NewCard("🖥 "+s.ixWhere(ctx, ix)+" 在终端里等你", feishubot.Orange, "终端里等你")
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
func (s *Service) answeredCard(ctx context.Context, ix proto.Interaction, how string) *feishubot.Card {
	title := map[string]string{proto.IxQuestion: "❓ ", proto.IxPlan: "📋 ", proto.IxPermission: "🔐 ", proto.IxScreen: "🖥 "}[ix.Kind] + s.ixWhere(ctx, ix)
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

func (s *Service) ixWhere(ctx context.Context, ix proto.Interaction) string {
	where := ix.SessionTitle
	if where == "" {
		where = "Persona"
	}
	if d, ok := s.device(ctx, ix.MachineID); ok && where != "Persona" {
		where += " · " + d.Name
	}
	return where
}

// openButton opens the conversation in the web app the hub serves. Card links are documented for http(s) and AppLink
// only: a jarvis:// link (the iPhone app) might get the whole card refused, so it is not used.
func (s *Service) openButton(ctx context.Context, machine, session string) feishubot.Button {
	st, _ := s.feishuStatusKept(ctx)
	web := strings.TrimRight(st.PublicURL, "/")
	if session != "" {
		web = sessionLink(web, machine, session, s.isAssistantConversation(ctx, machine, session))
	}
	return feishubot.LinkBtn("打开网页版", web)
}

// webBase is the hub's web app (hub.json's public_url) without the trailing slash, "" when the hub does not say.
// It only reads feishu.status (which carries the address whether the bot is on or not): unlike feishuStatus it
// does not reconcile the bot's chat state, so the snapshot does not touch it.
func (s *Service) webBase(ctx context.Context) string {
	var st FeishuStatus
	if err := s.appCall(ctx, AppFeishu, FeishuStatusGet, map[string]any{}, &st); err != nil {
		return ""
	}
	return strings.TrimRight(st.PublicURL, "/")
}

// sessionLink is a conversation's page in the web app at web (web/src/lib/router.ts): a Persona conversation is
// /j/<id>, an agent /s/<machine>/<id>; "" when web is.
func sessionLink(web, machine, session string, assistant bool) string {
	if web == "" {
		return ""
	}
	if assistant {
		return web + "/j/" + url.PathEscape(session)
	}
	return web + "/s/" + url.PathEscape(machine) + "/" + url.PathEscape(session)
}

// todoLink is a todo's page in the web app at web, "" when web is.
func todoLink(web, id string) string {
	if web == "" {
		return ""
	}
	return web + "/todos?id=" + url.QueryEscape(id)
}

// --- card buttons ---

// FeishuAction is feishu.card_action: the owner pressed one of the bot's cards (the hub checked it is the owner);
// the answer is the toast and the card to show instead. Feishu waits 3 seconds for it.
func (s *Service) FeishuAction(ctx context.Context, p FeishuAction) (*feishubot.CardResponse, error) {
	var a feishubot.CardAction
	if err := json.Unmarshal(p.Event, &a); err != nil {
		return nil, badRequest(err)
	}
	return s.botCardAction(ctx, &a, p.Action), nil
}

// botCardAction answers a press; action is the owner's action for the card it presses (what answers it).
func (s *Service) botCardAction(ctx context.Context, a *feishubot.CardAction, action string) *feishubot.CardResponse {
	switch a.Str("a") {
	case "ix":
		return s.answerFromCard(ctx, a, action)
	case "switch":
		f, _ := s.feishuFeatures(ctx)
		kind, m, sess, title := a.Str("k"), a.Str("m"), a.Str("s"), a.Str("t")
		t := botTarget{Kind: kind, MachineID: m, SessionID: sess}
		if kind == targetMain {
			t = botTarget{Kind: targetMain}
		}
		if err := botAllowed(f, t); err != nil {
			return toast("error", err.Error())
		}
		s.setBotCurrent(t)
		return toast("success", "现在发给「"+clip(title, 30)+"」")
	case "start":
		if f, _ := s.feishuFeatures(ctx); !f.Agents {
			return toast("error", "在飞书里驱动 Agent 没有打开")
		}
		machine, cwd, prompt := a.Form("machine"), strings.TrimSpace(a.Form("cwd")), strings.TrimSpace(a.Form("prompt"))
		if machine == "" || cwd == "" {
			return toast("warning", "选好电脑、填上目录")
		}
		go s.botStartAgent(machine, cwd, prompt)
		return toast("info", "正在开…")
	}
	return toast("info", "这张卡片已经过期了")
}

func toast(kind, text string) *feishubot.CardResponse {
	return &feishubot.CardResponse{Toast: &feishubot.Toast{Type: kind, Content: text}}
}

// cardAnswerWait is how long a press waits for its answer to go through before it says 「正在处理…」: the hub has
// Feishu's 3 seconds, and the press came over the bus.
var cardAnswerWait = 1500 * time.Millisecond

// answerFromCard turns a press on an interaction card into the same answer the app would give, and resolves it.
func (s *Service) answerFromCard(ctx context.Context, a *feishubot.CardAction, action string) *feishubot.CardResponse {
	id := a.Str("id")
	var got struct {
		Interaction proto.Interaction `json:"interaction"`
		OK          bool              `json:"ok"`
	}
	if err := s.appCall(ctx, AppInteractions, InteractionsGet, map[string]any{"interaction_id": id}, &got); err != nil {
		return toast("error", clip(oneLine(err.Error()), 80))
	}
	if !got.OK {
		return toast("info", "已经处理过了")
	}
	ix := got.Interaction
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
	b := &s.fbot
	b.mu.Lock()
	card, had := b.cards[id]
	delete(b.cards, id)
	b.mu.Unlock()
	restore := func() {
		if had {
			b.mu.Lock()
			if b.cards == nil {
				b.cards = map[string]botCard{}
			}
			b.cards[id] = card
			b.mu.Unlock()
		}
	}
	how := resolutionText(ix, r)
	type outcome struct {
		ok  bool
		err error
	}
	// Screen and Codex cards act on the terminal and can take a few seconds; Feishu waits 3 s for this answer.
	done := make(chan outcome, 1)
	go func() {
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ok, err := s.resolveInteraction(rctx, id, r, action)
		done <- outcome{ok, err}
	}()
	select {
	case o := <-done:
		switch {
		case o.err != nil:
			restore()
			return toast("error", clip(oneLine(o.err.Error()), 80))
		case !o.ok:
			return toast("info", "已经处理过了")
		}
		return &feishubot.CardResponse{Toast: &feishubot.Toast{Type: "success", Content: how}, Card: s.answeredCard(ctx, ix, how).Raw()}
	case <-time.After(cardAnswerWait):
		msg := a.Context.MessageID
		go func() {
			o := <-done
			if o.err != nil || !o.ok {
				if o.err != nil {
					s.logf("bots: answer %s: %v", id, o.err)
				}
				restore()
				return
			}
			if msg != "" {
				cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				s.botCardDone(cctx, msg, ix, how)
			}
		}()
		return toast("info", "正在处理…")
	}
}

// botStartAgent starts a coding agent from the /start card and points the chat at it.
func (s *Service) botStartAgent(machine, cwd, prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	d, ok := s.device(ctx, machine)
	if !ok {
		s.botSay(ctx, "没有这台电脑。")
		return
	}
	if strings.HasPrefix(cwd, "~/") && d.Home != "" {
		cwd = strings.TrimRight(d.Home, "/") + "/" + cwd[2:]
	}
	params := map[string]any{"cwd": cwd, "prompt": prompt, "tab_name": s.tabName(ctx, prompt)} // agents_start.go
	if err := s.applyModel(ctx, "agent.new", params); err != nil {
		s.botSay(ctx, "开不了："+err.Error())
		return
	}
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := s.call(ctx, d.ID, DeviceSessionStart, params, &res); err != nil {
		s.botSay(ctx, "开不了："+clip(oneLine(err.Error()), 200))
		return
	}
	t := botTarget{Kind: targetAgent, MachineID: d.ID, SessionID: res.Session.SessionID}
	label := agentLabel(res.Session)
	if label == "" || label == "." {
		label = cwd
	}
	label += " · " + d.Name
	s.setBotCurrent(t)
	c := feishubot.NewCard("", "", "已开始").Markdown("在 **" + feishubot.EscapeMarkdown(d.Name) + "** 的 " + feishubot.Code(cwd) + " 开了一个 Agent，现在发给它。")
	if prompt != "" {
		c.Note("它已经在做了，做完把结果发到这里。")
	}
	mid, err := s.botPost(ctx, "", c.Buttons(s.openButton(ctx, d.ID, res.Session.SessionID)), "")
	if err != nil {
		s.logf("bots: started note: %v", err)
	}
	s.rememberBotThread(mid, t, label)
	if prompt != "" && mid != "" {
		s.startChannelRun(&channel.Run{ID: "fb_" + mid, Channel: channel.Feishu, ReplyTo: mid, MachineID: t.MachineID, SessionID: t.SessionID,
			TargetKind: t.Kind, Label: label, Started: time.Now().UTC()})
	}
}

// --- pushes and the sync mirror ---

// feishuPush sends what notify_user pushed to the phone to Feishu too (the pushes switch).
func (s *Service) feishuPush(machine, session, title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if f, ready := s.feishuFeatures(ctx); !ready || !f.Pushes {
		return
	}
	c := feishubot.NewCard("🔔 "+clip(oneLine(title), 60), feishubot.Wathet, title).Markdown(feishubot.CardMarkdown(body))
	if session != "" {
		c.Buttons(s.openButton(ctx, machine, session))
	}
	mid, err := s.botPost(ctx, "", c, body)
	if err != nil {
		s.logf("bots: push: %v", err)
		return
	}
	if session != "" {
		s.rememberBotThread(mid, botTarget{Kind: targetJarvis, MachineID: machine, SessionID: session}, title)
	}
}

// feishuMirror sends a turn that ended without being asked for in Feishu: the main conversation's when sync is on
// (what was said in the app, a scheduled task's run), a scheduled task's own conversation's when pushes is on.
func (s *Service) feishuMirror(ctx context.Context, machine, session string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	f, ready := s.feishuFeatures(ctx)
	if !ready || !(f.Sync || f.Pushes) {
		return
	}
	main := s.isMainOn(ctx, machine, session)
	task := s.taskOfConversation(ctx, machine, session)
	switch {
	case main && f.Sync:
	case task != "" && f.Pushes:
	default:
		return
	}
	if s.pushedLately(session) && f.Pushes {
		return // its notify_user already went to Feishu
	}
	sess, ok := s.session(ctx, machine, session)
	if !ok || sess.WokeByTask && !main {
		return
	}
	asked, answer, ok := s.channelLastTurn(ctx, machine, session)
	if !ok {
		return
	}
	if strings.Contains(asked, "title=\"飞书机器人\"") {
		return // its own message: answered by the run
	}
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
	c.Note(label).Buttons(s.openButton(ctx, machine, session))
	mid, err := s.botPost(ctx, "", c, pieces[0])
	if err != nil {
		s.logf("bots: mirror: %v", err)
		return
	}
	s.rememberBotThread(mid, botTarget{Kind: targetJarvis, MachineID: machine, SessionID: session}, label)
}

// --- commands (docs/bots.md「命令」): which conversation or agent messages go to, the lists to pick one from,
// starting an agent, stopping a turn ---

func botHelp(f FeishuFeatures) string {
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
func (s *Service) botCommand(ctx context.Context, f FeishuFeatures, messageID, text string) (bool, string) {
	cmd, rest, _ := strings.Cut(strings.TrimSpace(text), " ")
	rest = strings.TrimSpace(rest)
	reply := func(c *feishubot.Card) {
		if _, err := s.botPost(ctx, messageID, c, ""); err != nil {
			s.logf("bots: command reply: %v", err)
		}
	}
	switch strings.ToLower(cmd) {
	case "/help", "/帮助":
		reply(feishubot.NewCard("怎么用", feishubot.Blue, "怎么用").Markdown(botHelp(f)))
	case "/main", "/jarvis", "/主对话":
		if !f.Main {
			s.botNote(ctx, messageID, "和 Persona 对话没有打开。")
			return true, ""
		}
		s.setBotCurrent(botTarget{Kind: targetMain})
		s.botNote(ctx, messageID, "好，现在发给主对话。")
	case "/new", "/新对话":
		if !f.Main {
			s.botNote(ctx, messageID, "和 Persona 对话没有打开。")
			return true, ""
		}
		s.setBotCurrent(botTarget{Kind: targetJarvis})
		if rest == "" {
			s.botNote(ctx, messageID, "好，下一条消息开一段新对话。发 /main 回到主对话。")
			return true, ""
		}
		return false, rest // "/new 帮我…": the rest starts it
	case "/list", "/对话":
		if !f.Main {
			s.botNote(ctx, messageID, "和 Persona 对话没有打开。")
			return true, ""
		}
		reply(s.conversationsCard(ctx))
	case "/agents", "/agent":
		if !f.Agents {
			s.botNote(ctx, messageID, "在飞书里驱动 Agent 没有打开（App 设置 › 连接器 › 飞书机器人）。")
			return true, ""
		}
		reply(s.agentsCard(ctx, rest))
	case "/start", "/开工":
		if !f.Agents {
			s.botNote(ctx, messageID, "在飞书里驱动 Agent 没有打开（App 设置 › 连接器 › 飞书机器人）。")
			return true, ""
		}
		reply(s.startAgentCard(ctx, rest))
	case "/stop", "/停":
		rt, err := s.resolveBotTarget(ctx, s.botTargetFor("", ""))
		if err != nil || rt.isNew {
			s.botNote(ctx, messageID, "现在没有在跑的。")
			return true, ""
		}
		if err := s.call(ctx, rt.machine, "session.interrupt", map[string]any{"session_id": rt.session.SessionID}, nil); err != nil {
			s.botNote(ctx, messageID, "没停下来："+clip(oneLine(err.Error()), 200))
			return true, ""
		}
		s.botNote(ctx, messageID, "停下了「"+rt.label+"」这一轮。")
	case "/status", "/状态":
		var b strings.Builder
		fmt.Fprintf(&b, "现在发给：**%s**\n", feishubot.EscapeMarkdown(s.targetLabel(ctx, s.botTargetFor("", ""))))
		if running := len(s.channelRuns(channel.Feishu)); running > 0 {
			fmt.Fprintf(&b, "在等 %d 个回答。\n", running)
		}
		reply(feishubot.NewCard("", "", "状态").Markdown(strings.TrimSpace(b.String())))
	default:
		return false, text
	}
	return true, ""
}

// conversationsCard lists recent Persona conversations with a button to switch to each.
func (s *Service) conversationsCard(ctx context.Context) *feishubot.Card {
	list, main := s.conversations(ctx, false)
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	c := feishubot.NewCard("最近的对话", feishubot.Blue, "最近的对话")
	if len(list) == 0 {
		return c.Markdown("还没有对话。直接发消息就会开始主对话。")
	}
	for i, sess := range list {
		if i == 8 {
			break
		}
		name := conversationName(sess, main)
		line := "**" + feishubot.EscapeMarkdown(name) + "**"
		if !sess.UpdatedAt.IsZero() {
			line += " · " + ago(sess.UpdatedAt)
		}
		if sess.LastText != "" {
			line += "\n" + feishubot.EscapeMarkdown(clip(oneLine(sess.LastText), 80))
		}
		kind := targetJarvis
		if sess.SessionID == main {
			kind = targetMain
		}
		c.Markdown(line).Buttons(feishubot.Btn("在这里聊", feishubot.Default, map[string]any{"a": "switch", "k": kind, "m": sess.MachineID, "s": sess.SessionID, "t": name}))
	}
	return c.Note("发 /new 开一段新对话")
}

// agentsCard lists the agents worth talking to: needing you, working, finished unseen, then the ones idle lately.
func (s *Service) agentsCard(ctx context.Context, query string) *feishubot.Card {
	type row struct {
		d    Device
		sess proto.Session
	}
	var rows []row
	q := strings.ToLower(query)
	for _, d := range s.devices(ctx) {
		list, _ := s.sessions(ctx, d.ID)
		for _, sess := range list {
			if sess.Assistant || sess.State == proto.StateOffline || sess.State == proto.StateUnknown {
				continue
			}
			if sess.State == proto.StateIdle && time.Since(sess.UpdatedAt) > 48*time.Hour {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(sess.Title+" "+sess.Cwd+" "+sess.SpaceName+" "+sess.SlotName+" "+d.Name), q) {
				continue
			}
			rows = append(rows, row{d, sess})
		}
	}
	order := map[string]int{proto.StateNeedsYou: 0, proto.StateWorking: 1, proto.StateUnread: 2, proto.StateIdle: 3}
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oj := order[rows[i].sess.State], order[rows[j].sess.State]
		if oi != oj {
			return oi < oj
		}
		return rows[i].sess.UpdatedAt.After(rows[j].sess.UpdatedAt)
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
		label := agentLabel(r.sess)
		line := icons[r.sess.State] + " **" + feishubot.EscapeMarkdown(label) + "** · " + feishubot.EscapeMarkdown(r.d.Name) + " · " + stateNames[r.sess.State]
		if r.sess.LastText != "" {
			line += "\n" + feishubot.EscapeMarkdown(clip(oneLine(r.sess.LastText), 90))
		}
		c.Markdown(line).Buttons(feishubot.Btn("在这里聊", feishubot.Default,
			map[string]any{"a": "switch", "k": targetAgent, "m": r.d.ID, "s": r.sess.SessionID, "t": label + " · " + r.d.Name}))
	}
	return c
}

// startAgentCard is a form to start a coding agent on a machine.
func (s *Service) startAgentCard(ctx context.Context, prompt string) *feishubot.Card {
	var opts []feishubot.Option
	for _, d := range s.devices(ctx) {
		if d.Online {
			opts = append(opts, feishubot.Option{Label: d.Name, Value: d.ID})
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
