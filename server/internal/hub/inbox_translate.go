package hub

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/ai"
)

// New mail is translated as it comes in, not when it is opened (docs/ios.md「翻译」, 2026-11-01): after each mail
// collection, open mail items whose text is in another script than a target some device asked for (设置 › 翻译 sent to
// the hub with the device's prefs) get the translation stored on the item. The list shows the translated subject and
// preview; opening the mail finds the whole translation there (translateMail).

// InboxTranslation is one item's translation into one language.
type InboxTranslation struct {
	Subject          string `json:"subject"`
	Preview          string `json:"preview"`
	Text             string `json:"text,omitempty"` // the whole body; not in the list
	Clipped          bool   `json:"clipped,omitempty"`
	ModelFingerprint string `json:"model_fingerprint,omitempty"`
	Of               string `json:"of,omitempty"` // the item's Last it translates
}

// foreignScript reports whether text is in another writing system than target's. Scripts, not languages: English
// mail is translated for a Chinese target, French is not for an English one (both Latin). Too little text says no.
func foreignScript(text, target string) bool {
	var han, kana, hangul, latin, cyrillic, letters int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case unicode.Is(unicode.Latin, r):
			latin++
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		default:
			continue
		}
		letters++
	}
	if letters < 12 {
		return false
	}
	share := func(n int) float64 { return float64(n) / float64(letters) }
	switch strings.SplitN(target, "-", 2)[0] {
	case "zh":
		// Latin words inside Chinese mail (names, code) are common: Chinese is a fifth of the letters at least.
		return share(han) < 0.2 || share(kana) > 0.1
	case "ja":
		return share(kana) < 0.05
	case "ko":
		return share(hangul) < 0.2
	case "ru":
		return share(cyrillic) < 0.3
	default:
		return share(latin) < 0.5
	}
}

type translateJob struct {
	it      *InboxItem
	target  string
	account string
	mailbox string
	uid     uint32
	last    string
}

// translateInbox translates what needs it, in the background, one pass at a time.
func (h *Hub) translateInbox(ctx context.Context) {
	targets := h.push.mailTargets()
	if len(targets) == 0 || h.mail == nil || h.translateModel() != nil || !h.inbox.translating.TryLock() {
		return
	}
	fingerprint, err := h.modelFingerprint(ctx, "translate")
	if err != nil {
		h.inbox.translating.Unlock()
		return
	}
	h.inbox.mu.Lock()
	var jobs []translateJob
	for _, it := range h.inbox.items {
		if it.Mail == nil || !it.open() || len(it.Mail.UIDs) == 0 || it.BodyOf != it.Last {
			continue
		}
		for _, t := range targets {
			if have := it.Translations[t]; have != nil && have.Of == it.Last && have.ModelFingerprint == fingerprint {
				continue
			}
			if !foreignScript(it.Title+"\n"+it.Preview, t) {
				continue
			}
			jobs = append(jobs, translateJob{it, t, it.Mail.AccountID, it.Mail.Mailbox, it.Mail.UIDs[len(it.Mail.UIDs)-1], it.Last})
		}
	}
	h.inbox.mu.Unlock()
	if len(jobs) == 0 {
		h.inbox.translating.Unlock()
		return
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].it.UpdatedAt.After(jobs[j].it.UpdatedAt) }) // newest first
	go func() {
		defer h.inbox.translating.Unlock()
		for _, j := range jobs {
			if ctx.Err() != nil {
				return
			}
			cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			t, err := h.translateMail(cctx, j.account, j.uid, j.mailbox, j.target)
			cancel()
			if err != nil {
				if ai.IsPaused(err) { // the rest wait for the next pass
					return
				}
				h.logf("inbox: translate %s into %s: %v", j.it.ID, j.target, err)
				continue
			}
			h.inbox.mu.Lock()
			if j.it.Last != j.last { // new mail in the thread meanwhile: the next pass
				h.inbox.mu.Unlock()
				continue
			}
			if j.it.Translations == nil {
				j.it.Translations = map[string]*InboxTranslation{}
			}
			j.it.Translations[j.target] = &InboxTranslation{Subject: t.Subject, Preview: clip(oneLine(t.Text), 200), Text: t.Text,
				Clipped: t.Clipped, Of: j.last, ModelFingerprint: t.ModelFingerprint}
			h.inbox.mu.Unlock()
			h.saveInbox(ctx, []*InboxItem{j.it})
		}
	}()
}

// storedMailTranslation is a translation the inbox already has for this message, if any.
func (h *Hub) storedMailTranslation(accountID string, uid uint32, mailbox, target string) (mailTranslation, bool) {
	fingerprint, err := h.modelFingerprint(context.Background(), "translate")
	if err != nil {
		return mailTranslation{}, false
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	for _, it := range h.inbox.items {
		m := it.Mail
		if m == nil || m.AccountID != accountID || len(m.UIDs) == 0 || m.UIDs[len(m.UIDs)-1] != uid || (mailbox != "" && m.Mailbox != mailbox) {
			continue
		}
		if t := it.Translations[target]; t != nil && t.Of == it.Last && t.Text != "" && t.ModelFingerprint == fingerprint {
			return mailTranslation{Subject: t.Subject, Text: t.Text, Clipped: t.Clipped, ModelFingerprint: fingerprint}, true
		}
	}
	return mailTranslation{}, false
}
