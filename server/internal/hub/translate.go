package hub

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/prompts"
)

// Translation (docs/ios.md「翻译」): the app decides what needs translating (its own setting, its own language
// detection) and asks the hub, which uses the small-task model (inbox.triage_ai: a cheap one is enough) and keeps
// the answers in memory, so opening the same mail again is instant. Mail is translated by reference (the hub reads
// the message itself), short texts (Feishu messages of chosen chats) in batches.

const (
	translateBatch    = 30    // texts in one request
	translateText     = 2000  // characters of one short text
	translateMailText = 12000 // characters of a mail body translated
	translateCacheMax = 4000
)

type translateCache struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *translateCache) get(k string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *translateCache) put(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= translateCacheMax {
		c.m = map[string]string{} // crude, but a day's worth of mail fits many times over
	}
	c.m[k] = v
}

func translateKey(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// languageNames are the targets the app offers (BCP 47, as the app's NaturalLanguage detection names them).
var languageNames = map[string]string{
	"zh-Hans": "简体中文", "zh-Hant": "繁體中文", "en": "English", "ja": "日本語", "ko": "한국어",
	"fr": "Français", "de": "Deutsch", "es": "Español", "ru": "Русский", "pt": "Português",
}

func (h *Hub) translateModel() error {
	if h.triageModel() == nil {
		return errors.New("hub.json has no ai model configured for translation")
	}
	return nil
}

// translateTexts translates short texts, keeping their order; cached ones are not sent again.
func (h *Hub) translateTexts(ctx context.Context, texts []string, target string) ([]string, error) {
	lang, ok := languageNames[target]
	if !ok {
		return nil, fmt.Errorf("unknown target language %q", target)
	}
	if err := h.translateModel(); err != nil {
		return nil, err
	}
	fingerprint, err := h.modelFingerprint(ctx, "translate")
	if err != nil {
		return nil, err
	}
	out := make([]string, len(texts))
	var todo []int
	for i, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if v, ok := h.translations.get(translateKey("t", fingerprint, target, t)); ok {
			out[i] = v
		} else {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return out, nil
	}
	in := make([]string, len(todo))
	total := 0
	for j, i := range todo {
		in[j] = clip(strings.TrimSpace(texts[i]), translateText)
		total += len([]rune(in[j]))
	}
	body, _ := json.Marshal(map[string]any{"texts": in})
	system := prompts.RenderTranslateTexts(lang)
	var res struct {
		Texts []string `json:"texts"`
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// A reasoning model spends hidden tokens first (docs/lessons/agents.md): leave plenty.
	if err := h.purposeModel("translate").JSONMax(cctx, system, string(body), min(4000+total*3, 32000), &res); err != nil {
		return nil, err
	}
	if len(res.Texts) != len(in) {
		return nil, fmt.Errorf("the model returned %d texts for %d", len(res.Texts), len(in))
	}
	for j, i := range todo {
		v := strings.TrimSpace(res.Texts[j])
		out[i] = v
		if v != "" {
			h.translations.put(translateKey("t", fingerprint, target, strings.TrimSpace(texts[i])), v)
		}
	}
	return out, nil
}

type mailTranslation struct {
	ModelFingerprint string `json:"model_fingerprint,omitempty"`
	Subject          string `json:"subject"`
	Text             string `json:"text"`
	Clipped          bool   `json:"clipped,omitempty"` // the body was longer than translated
}

// translateMail translates one message's subject and body (the HTML version as readable text when there is no text
// part).
func (h *Hub) translateMail(ctx context.Context, accountID string, uid uint32, mailbox, target string) (mailTranslation, error) {
	lang, ok := languageNames[target]
	if !ok {
		return mailTranslation{}, fmt.Errorf("unknown target language %q", target)
	}
	if h.mail == nil {
		return mailTranslation{}, errors.New("mail is not set up")
	}
	if err := h.translateModel(); err != nil {
		return mailTranslation{}, err
	}
	if t, ok := h.storedMailTranslation(accountID, uid, mailbox, target); ok { // translated when it came in
		return t, nil
	}
	fingerprint, err := h.modelFingerprint(ctx, "translate")
	if err != nil {
		return mailTranslation{}, err
	}
	key := translateKey("m", fingerprint, accountID, mailbox, fmt.Sprint(uid), target)
	if v, ok := h.translations.get(key); ok {
		var t mailTranslation
		if json.Unmarshal([]byte(v), &t) == nil {
			return t, nil
		}
	}
	m, err := h.mail.GetMessage(ctx, accountID, uid, mailbox)
	if err != nil {
		return mailTranslation{}, err
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		text = mail.ReadableText(m.HTML)
	}
	var t mailTranslation
	if r := []rune(text); len(r) > translateMailText {
		text, t.Clipped = string(r[:translateMailText]), true
	}
	system := prompts.RenderTranslateMail(lang)
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	in := prompts.RenderTranslateMailInput(m.Subject, text)
	if err := h.purposeModel("translate").JSONMax(cctx, system, in, min(4000+len([]rune(in))*3, 48000), &t); err != nil {
		return mailTranslation{}, err
	}
	t.Subject, t.Text = strings.TrimSpace(t.Subject), strings.TrimSpace(t.Text)
	if t.Text == "" && text != "" {
		return mailTranslation{}, errors.New("the model returned no translation")
	}
	t.ModelFingerprint = fingerprint
	raw, _ := json.Marshal(t)
	h.translations.put(key, string(raw))
	return t, nil
}

func (h *Hub) translateRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/translate", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Texts  []string `json:"texts"`
			Target string   `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.Texts) == 0 || len(in.Texts) > translateBatch {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("texts: 1 to %d", translateBatch))
			return
		}
		out, err := h.translateTexts(r.Context(), in.Texts, in.Target)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"texts": out})
	})
	api.HandleFunc("POST /api/v1/translate/mail", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AccountID string `json:"account_id"`
			UID       uint32 `json:"uid"`
			Mailbox   string `json:"mailbox"`
			Target    string `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AccountID == "" || in.UID == 0 {
			writeErr(w, http.StatusBadRequest, errors.New("account_id and uid are required"))
			return
		}
		if in.Mailbox == "" {
			in.Mailbox = "INBOX"
		}
		t, err := h.translateMail(r.Context(), in.AccountID, in.UID, in.Mailbox, in.Target)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, t)
	})
}
