package hub

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/prompts"
)

// InboxConfig tunes the inbox (hub.json "inbox").
type InboxConfig struct {
	// Rules is extra guidance for triage, in the user's words ("anything from my manager needs me", "alerts from
	// group X are noise"…), appended to the prompt.
	Rules string `json:"rules,omitempty"`
	// TriageAI (model 1) sorts and summarizes new items; JudgeAI (model 2) decides which ones the agent handles.
	// Empty fields fall back to hub.json's "ai"; without them both use "ai".
	TriageAI *ai.Config `json:"triage_ai,omitempty"`
	JudgeAI  *ai.Config `json:"judge_ai,omitempty"`
	// Agent (model 3) works on the items the judge hands over and drafts the reply (inbox_agent.go).
	Agent InboxAgentConfig `json:"agent,omitempty"`
}

func (h *Hub) triageModel() *ai.Client {
	return h.purposeModel("inbox.triage")
}

const triageConcurrency = 4

// triageInbox sorts open items whose content changed since they were last looked at into 需要你 / 知悉即可 / 噪音
// with a one-line summary (the hub's small-task model, ai.go). It runs in the background; one pass at a time.
func (h *Hub) triageInbox(ctx context.Context) {
	if h.triageModel() == nil || !h.inbox.triaging.TryLock() {
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
		h.handInbox(ctx)
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
		h.handInbox(ctx)
	}()
}

// triageOne asks the model about one item; when the model fails the item gets the rule-based tier (and no summary).
func (h *Hub) triageOne(ctx context.Context, it *InboxItem) bool {
	h.inbox.mu.Lock()
	last, input := it.Last, triageInput(it)
	h.inbox.mu.Unlock()
	system := prompts.RenderInboxTriage(prompts.InboxTriage{Owner: h.cfg.Assistant.Owner, Rules: strings.TrimSpace(h.cfg.Inbox.Rules)})
	var out struct {
		Tier    string `json:"tier"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	err := h.triageModel().JSON(cctx, system, input, &out)
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
	case KindDM, KindMention, KindDoc:
		return TierAction
	case KindMoment: // an @ asks for me; a comment or a reply is mostly chatter
		if it.Moment != nil && strings.HasPrefix(it.Moment.Notice, "atIn") {
			return TierAction
		}
		return TierFYI
	case KindGroup:
		return TierFYI
	case KindMail:
		if it.Mail != nil && it.Mail.Direct {
			return TierFYI
		}
	}
	return TierNoise
}

// triageInput is the item as the models read it (prompts/inbox/item.md).
func triageInput(it *InboxItem) string { return prompts.RenderInboxItem(promptItem(it)) }

func promptItem(it *InboxItem) prompts.InboxItem {
	return prompts.InboxItem{Kind: it.Kind, MailDirect: it.Mail != nil && it.Mail.Direct, Context: it.Context}
}

// draftReply writes a reply to an inbox item in the user's voice (optionally following their instruction).
func (h *Hub) draftReply(ctx context.Context, id, instruction string) (string, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var item prompts.InboxItem
	if ok {
		item = promptItem(it)
	}
	h.inbox.mu.Unlock()
	if !ok {
		return "", errNoInboxItem
	}
	system := prompts.RenderInboxDraft(prompts.InboxDraft{Owner: h.cfg.Assistant.Owner, Notes: strings.TrimSpace(h.cfg.Assistant.Notes)})
	input := prompts.RenderInboxDraftInput(prompts.InboxDraftInput{Item: item, Instruction: strings.TrimSpace(instruction)})
	text, err := h.purposeModel("inbox.draft").Complete(ctx, system, input, 1024)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// replyInbox sends text as the user's reply — in the Feishu chat, or as a reply to the newest mail of the thread
// from the account that received it — and files the item as done.
func (h *Hub) replyInbox(ctx context.Context, id, text string, changes ...DocChange) (InboxItem, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var c InboxItem
	if ok {
		c = *it
		if it.Mail != nil {
			m := *it.Mail
			c.Mail = &m
		}
		if it.Doc != nil {
			d := *it.Doc
			c.Doc = &d
		}
		if it.Moment != nil {
			m := *it.Moment
			c.Moment = &m
		}
	}
	h.inbox.mu.Unlock()
	if !ok {
		return InboxItem{}, errNoInboxItem
	}
	switch {
	case c.Moment != nil && len(changes) == 0:
		if err := h.replyMoment(ctx, *c.Moment, text); err != nil {
			return InboxItem{}, err
		}
	case c.Doc != nil:
		// The status moves first: a reply saying "done" with the row still 进行中 would be wrong.
		for _, ch := range changes {
			if err := h.writeDocStatus(ctx, *c.Doc, ch); err != nil {
				return InboxItem{}, err
			}
		}
		if text != "" {
			if err := h.replyDoc(ctx, *c.Doc, text); err != nil {
				return InboxItem{}, err
			}
		}
	case len(changes) > 0:
		return InboxItem{}, errors.New("only document items have a status to change")
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
