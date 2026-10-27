package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/mail"
)

// InboxConfig tunes the inbox (hub.json "inbox").
type InboxConfig struct {
	// Rules is extra guidance for triage, in the user's words ("anything from my manager needs me", "alerts from
	// group X are noise"…), appended to the prompt.
	Rules string `json:"rules,omitempty"`
}

const triageConcurrency = 4

const triagePrompt = `你是用户（称呼：%s）的私人助理，替用户分拣收到的工作消息（飞书）和邮件，让用户只看需要自己的事。对给你的这一件事判断：

- action（需要你）：需要用户本人回复、决定、处理，或者用户应该尽快知道。例如：有人直接问用户问题、@用户要用户做事或表态、私聊里找用户、
  审批 / 确认 / 评审请求、和用户负责的东西有关的故障告警、需要回复的邮件、截止时间临近的事。
- fyi（知悉即可）：和用户有关、值得扫一眼，但不需要用户做什么。例如：群里同步的进展和结论、抄送给用户的邮件、有信息量的通知。
- noise（噪音）：营销、订阅、newsletter、广告、自动化通知、和用户无关的群聊闲聊、机器人刷屏。

拿不准 action 还是 fyi 时选 action；私聊和 @用户 的默认是 action，除非内容明显只是通知或寒暄。

summary：一句中文（不超过 40 个字），说清楚这件事是什么、要用户做什么，比如「刘森问压测目标，等你确认是否只测 code 沙箱」。
不要写「有人发了消息」这种空话，不要加引号和标点以外的修饰。
reason：不超过 12 个字，为什么这么分。

回复 JSON：{"tier":"action|fyi|noise","summary":"…","reason":"…"}`

// triageInbox sorts open items whose content changed since they were last looked at into 需要你 / 知悉即可 / 噪音
// with a one-line summary (the hub's small-task model, ai.go). It runs in the background; one pass at a time.
func (h *Hub) triageInbox(ctx context.Context) {
	if h.ai == nil || !h.inbox.triaging.TryLock() {
		return
	}
	h.inbox.mu.Lock()
	var todo []*InboxItem
	for _, it := range h.inbox.items {
		if it.open() && it.TriagedAt != it.Last && it.Context != "" {
			todo = append(todo, it)
		}
	}
	h.inbox.mu.Unlock()
	if len(todo) == 0 {
		h.inbox.triaging.Unlock()
		return
	}
	go func() {
		defer h.inbox.triaging.Unlock()
		sem := make(chan struct{}, triageConcurrency)
		var wg sync.WaitGroup
		for _, it := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func(it *InboxItem) {
				defer wg.Done()
				defer func() { <-sem }()
				if h.triageOne(ctx, it) {
					h.saveInbox(ctx, []*InboxItem{it})
				}
			}(it)
		}
		wg.Wait()
	}()
}

// triageOne asks the model about one item; when the model fails the item gets the rule-based tier (and no summary).
func (h *Hub) triageOne(ctx context.Context, it *InboxItem) bool {
	h.inbox.mu.Lock()
	last, input := it.Last, triageInput(it)
	h.inbox.mu.Unlock()
	owner := h.cfg.Assistant.Owner
	if owner == "" {
		owner = "你"
	}
	system := fmt.Sprintf(triagePrompt, owner)
	if r := strings.TrimSpace(h.cfg.Inbox.Rules); r != "" {
		system += "\n\n用户自己定的规则（优先于上面的默认判断）：\n" + r
	}
	var out struct {
		Tier    string `json:"tier"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	err := h.ai.JSON(cctx, system, input, &out)
	cancel()
	if ctx.Err() != nil {
		return false
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if it.Last != last { // new messages arrived meanwhile: the next pass sees them
		return false
	}
	switch out.Tier {
	case TierAction, TierFYI, TierNoise:
		it.Tier, it.Summary, it.Reason = out.Tier, clip(oneLine(out.Summary), 80), clip(oneLine(out.Reason), 30)
	default:
		if err != nil {
			h.logf("inbox: triage %s: %v", it.ID, err)
		}
		it.Tier, it.Reason = ruleTier(it), "按规则"
	}
	it.TriagedAt = last
	// Triage can settle an item read elsewhere while it was still untriaged.
	if it.Read && it.Tier != TierAction {
		it.done(time.Now(), "read")
	}
	return true
}

// ruleTier is the tier without a model: people talking to me need me, groups are FYI, bots and lists are noise.
func ruleTier(it *InboxItem) string {
	switch it.Kind {
	case KindDM, KindMention:
		return TierAction
	case KindGroup:
		return TierFYI
	case KindMail:
		if it.Mail != nil && it.Mail.Direct {
			return TierFYI
		}
	}
	return TierNoise
}

func triageInput(it *InboxItem) string {
	var b strings.Builder
	switch it.Kind {
	case KindDM:
		b.WriteString("类型：飞书私聊\n")
	case KindMention:
		b.WriteString("类型：飞书群里 @ 了用户\n")
	case KindGroup:
		b.WriteString("类型：飞书群的新消息（没有 @ 用户）\n")
	case KindBot:
		b.WriteString("类型：飞书机器人 / 应用发来的消息\n")
	case KindMail:
		if it.Mail != nil && it.Mail.Direct {
			b.WriteString("类型：邮件（直接发给用户）\n")
		} else {
			b.WriteString("类型：邮件（抄送、群发或邮件列表）\n")
		}
	}
	b.WriteString(it.Context)
	return b.String()
}

const draftPrompt = `你替用户（称呼：%s）起草一条回复，用户看过、改过之后自己发出去。

- 用用户的口吻，第一人称，简洁自然，直接回应对方要的东西；不要解释你在做什么，只输出回复正文。
- 飞书消息：口语、短，像同事之间聊天，不要「您好」「此致」这类客套；群里回复时不用 @ 对方（发送时会在原会话里）。
- 邮件：按来信的语言和正式程度，可以有简短的称呼和落款；营销邮件一般不需要回，真要回也要短。
- 不知道的事实（时间、数字、结论）不要编，用【】标出来让用户补，比如「【周几】之前给你」。`

// draftReply writes a reply to an inbox item in the user's voice (optionally following their instruction).
func (h *Hub) draftReply(ctx context.Context, id, instruction string) (string, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var input string
	if ok {
		input = triageInput(it)
	}
	h.inbox.mu.Unlock()
	if !ok {
		return "", errNoInboxItem
	}
	owner := h.cfg.Assistant.Owner
	if owner == "" {
		owner = "你"
	}
	system := fmt.Sprintf(draftPrompt, owner)
	if n := strings.TrimSpace(h.cfg.Assistant.Notes); n != "" {
		system += "\n\n关于用户：\n" + n
	}
	if s := strings.TrimSpace(instruction); s != "" {
		input += "\n\n用户希望这样回：" + s
	}
	text, err := h.ai.Complete(ctx, system, input, 1024)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// replyInbox sends text as the user's reply — in the Feishu chat, or as a reply to the newest mail of the thread
// from the account that received it — and files the item as done.
func (h *Hub) replyInbox(ctx context.Context, id, text string) (InboxItem, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var c InboxItem
	if ok {
		c = *it
		if it.Mail != nil {
			m := *it.Mail
			c.Mail = &m
		}
	}
	h.inbox.mu.Unlock()
	if !ok {
		return InboxItem{}, errNoInboxItem
	}
	switch {
	case c.Lark != nil:
		if err := h.lark.Send(ctx, c.Lark.FeedID, text); err != nil {
			return InboxItem{}, err
		}
	case c.Mail != nil && h.mail != nil && len(c.Mail.UIDs) > 0:
		orig, err := h.mail.GetMessage(ctx, c.Mail.AccountID, c.Mail.UIDs[len(c.Mail.UIDs)-1], c.Mail.Mailbox)
		if err != nil {
			return InboxItem{}, err
		}
		if err := h.mail.Send(ctx, mailReply(c.Mail.AccountID, orig, text)); err != nil {
			return InboxItem{}, err
		}
	default:
		return InboxItem{}, errors.New("this item cannot be replied to")
	}
	h.inbox.mu.Lock()
	it.done(time.Now(), "replied")
	out := it.listed()
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	return out, nil
}

// mailReply is a reply to orig: Re: subject, threading headers, the original quoted below.
func mailReply(accountID string, orig *mail.MailMessage, text string) mail.SendRequest {
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	refs := append(append([]string{}, orig.References...), orig.MessageID)
	var quoted []string
	for i, l := range strings.Split(orig.Text, "\n") {
		if i >= 40 {
			break
		}
		quoted = append(quoted, "> "+l)
	}
	from := orig.From.Name
	if from == "" {
		from = orig.From.Email
	}
	body := text + "\n\n" + orig.Date + "，" + from + " 写道：\n" + strings.Join(quoted, "\n")
	return mail.SendRequest{AccountID: accountID, To: []mail.Address{orig.From}, Subject: subject, Text: body,
		InReplyTo: orig.MessageID, References: strings.Join(refs, " ")}
}
