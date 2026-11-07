package hub

import (
	"context"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// The user's language (docs/protocol/i18n.md "Protocol"): what the latest full-role client said in client_hello.locale, kept in
// Postgres settings. Whatever the hub writes for people without a request in front of it reads in it: pushes to a
// device that gave no locale of its own, the assistant's answers, injected messages, task notifications, the bots'
// replies. Until a client says one it is zh-Hans, what the hub wrote before.

const userLocaleSetting = "user_locale"

func (h *Hub) loadUserLang(ctx context.Context) {
	if h.store == nil {
		return
	}
	var s string
	if _, err := h.store.Setting(ctx, userLocaleSetting, &s); err != nil {
		h.logf("load %s: %v", userLocaleSetting, err)
		return
	}
	if l := i18n.Lang(s); i18n.Valid(l) {
		h.userLocale.Store(l)
	}
}

// userLang is the user's language, zh-Hans when none was said.
func (h *Hub) userLang() i18n.Lang {
	if l, ok := h.userLocale.Load().(i18n.Lang); ok && i18n.Valid(l) {
		return l
	}
	return i18n.Default
}

// setUserLang records the language a full-role client reported; a change is saved and told to the assistant
// (its CLAUDE.md asks it to answer in this language).
func (h *Hub) setUserLang(l i18n.Lang) {
	if !i18n.Valid(l) || h.userLang() == l {
		return
	}
	h.userLocale.Store(l)
	if h.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.store.PutSetting(ctx, userLocaleSetting, string(l)); err != nil {
			h.logf("save %s: %v", userLocaleSetting, err)
		}
	}
	h.logf("user language: %s", l)
	h.assistantUpdated()
}

// requestLang is the language of a request's response: its Accept-Language, else the user's.
func (h *Hub) requestLang(r *http.Request) i18n.Lang {
	if l := i18n.From(r.Context()); l != "" {
		return l
	}
	return i18n.FromRequest(r).Or(h.userLang())
}

// lang is the language of what goes to this connection: what it said in client_hello, else its upgrade request's
// Accept-Language, else the user's.
func (c *client) lang() i18n.Lang {
	c.mu.Lock()
	l := i18n.Lang(c.meta.Locale)
	c.mu.Unlock()
	if i18n.Valid(l) {
		return l
	}
	if i18n.Valid(c.acceptLang) {
		return c.acceptLang
	}
	if c.hub != nil {
		return c.hub.userLang()
	}
	return i18n.Default
}

// contextLang is the language of what ctx is doing for people: its request's (i18n.With), else the user's.
func (h *Hub) contextLang(ctx context.Context) i18n.Lang {
	if l := i18n.From(ctx); i18n.Valid(l) {
		return l
	}
	if h == nil {
		return i18n.Default
	}
	return h.userLang()
}
