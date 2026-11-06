// Package wechat is the WeChat plugin's program (jarvisd-wechat, docs/wechat.md): a bot in the user's personal WeChat
// through iLink (wechat/ilink), contributed as a channel (docs/channels.md). The user binds it by scanning a QR code
// (the connector's sign-in); the owner's messages to the bot go to the hub (channel.receive), which sends them into a
// conversation, follows the turn and asks what agents ask — all of that is the hub's. This program only talks to
// WeChat: it receives, sends what the hub gives it (channel.send) and shows "typing…".
//
// It keeps its own in one sealed blob in the hub (the connector's secrets): the bot token, the polling cursor and the
// owner's latest context token, so a restart picks up where it was.
package wechat

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/wechat/ilink"
	"github.com/nwverse/persona/server/internal/wechat/spec"
)

// Clock and iLink are what tests replace.
type Options struct {
	// Hub takes the owner's messages (the plugin protocol's channel namespace).
	Hub   channel.Hub
	Agent string // the product token iLink sees ("Persona/<version>")
	// Lang is the user's language as the hub said it at this start: what the bot writes for people reads in it
	// (zh-Hans when not said).
	Lang i18n.Lang
	// NewClient makes the iLink client for a base URL and token (ilink.New when nil).
	NewClient func(base, token string) (*ilink.Client, error)
	Now       func() time.Time
}

// Bot is the connector and the bot behind it.
type Bot struct {
	opt     Options
	secrets connector.Secrets

	mu      sync.Mutex
	st      state
	loaded  bool
	status  connector.Status
	client  *ilink.Client // for the bound token; nil while not bound
	qr      *pendingQR
	wake    chan struct{}
	typing  *typing // the owner's chat shows the bot writing (the hub says while a turn goes on)
	replied map[string]time.Time
}

// state is what is kept in the hub (sealed): never logged.
type state struct {
	Creds *ilink.Credentials `json:"creds,omitempty"`
	// Old are bot tokens it had before (the last 10): a new authorization sends them so iLink knows it is this bot.
	Old    []string `json:"old,omitempty"`
	Cursor string   `json:"cursor,omitempty"`
	// Context is the owner's latest message's context token: iLink needs one to send anything.
	Context string `json:"context,omitempty"`
	// Seen are the ids of the latest messages handled (iLink may deliver one again).
	Seen []string `json:"seen,omitempty"`
}

func New(opt Options, secrets connector.Secrets) *Bot {
	if opt.NewClient == nil {
		opt.NewClient = func(base, token string) (*ilink.Client, error) {
			return ilink.New(ilink.Options{BaseURL: base, Token: token, Agent: opt.Agent})
		}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Bot{opt: opt, secrets: secrets, wake: make(chan struct{}, 1), replied: map[string]time.Time{},
		status: connector.Status{State: connector.StateNotSignedIn, Error: i18n.T(opt.Lang.Or(i18n.Default), "wechat-wechat.not_bound")}}
}

// lang is the language of what the bot writes for people.
func (b *Bot) lang() i18n.Lang { return b.opt.Lang.Or(i18n.Default) }

func (*Bot) ID() string                   { return spec.ID }
func (*Bot) Name() string                 { return spec.Name }
func (*Bot) Manifest() connector.Manifest { return spec.Manifest() }

// Status is answered from memory.
func (b *Bot) Status(context.Context) connector.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *Bot) setStatus(s connector.Status) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s.LastEvent = b.status.LastEvent
	b.status = s
}

// account is how the bound WeChat is shown, in l: iLink gives an id, not a name.
func account(l i18n.Lang, c *ilink.Credentials) string {
	id, _, _ := strings.Cut(c.UserID, "@")
	if r := []rune(id); len(r) > 6 {
		id = string(r[len(r)-6:])
	}
	return i18n.T(l, "wechat-wechat.account", i18n.Args{"id": id})
}

func (b *Bot) load(ctx context.Context) error {
	b.mu.Lock()
	if b.loaded {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()
	data, err := b.secrets.Load(ctx)
	if err != nil {
		return err
	}
	var st state
	if len(data) > 0 {
		if err := json.Unmarshal(data, &st); err != nil {
			log.Printf("wechat: the stored state does not read, starting over: %v", err)
			st = state{}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.loaded {
		b.st, b.loaded = st, true
	}
	return nil
}

// saveLocked stores the state; b.mu is held.
func (b *Bot) saveLocked(ctx context.Context) {
	data, err := json.Marshal(b.st)
	if err == nil {
		err = b.secrets.Save(ctx, data)
	}
	if err != nil {
		log.Printf("wechat: saving the state: %v", err)
	}
}

func (b *Bot) kick() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Run polls iLink for the owner's messages while bound.
func (b *Bot) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := b.load(ctx); err == nil {
			break
		} else {
			b.setStatus(connector.Status{State: connector.StateError, Error: i18n.T(b.lang(), "wechat-wechat.cannot_load", i18n.Args{"error": err.Error()})})
		}
		if !sleep(ctx, 5*time.Second) {
			return
		}
	}
	for ctx.Err() == nil {
		b.mu.Lock()
		creds := b.st.Creds
		b.mu.Unlock()
		if creds == nil {
			if b.Status(ctx).State != connector.StateNotSignedIn { // keep why it was unbound
				b.setStatus(connector.Status{State: connector.StateNotSignedIn, Error: i18n.T(b.lang(), "wechat-wechat.not_bound")})
			}
			select {
			case <-ctx.Done():
				return
			case <-b.wake:
			}
			continue
		}
		b.serve(ctx, *creds)
	}
}

var backoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// serve long-polls with creds until they change, are refused or ctx ends.
func (b *Bot) serve(ctx context.Context, creds ilink.Credentials) {
	c, err := b.opt.NewClient(creds.BaseURL, creds.Token)
	if err != nil {
		b.setStatus(connector.Status{State: connector.StateError, Account: account(b.lang(), &creds), Error: i18n.T(b.lang(), "wechat-wechat.bad_address", i18n.Args{"error": err.Error()})})
		b.unbind(ctx, "")
		return
	}
	b.mu.Lock()
	b.client = c
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.client = nil
		b.mu.Unlock()
	}()
	ok := connector.Status{State: connector.StateOK, Account: account(b.lang(), &creds), Via: i18n.T(b.lang(), "wechat-wechat.via")}
	if err := c.NotifyStart(ctx); err != nil {
		log.Printf("wechat: notify start: %v", err) // not needed to receive messages
	}
	b.setStatus(ok)
	failures := 0
	for ctx.Err() == nil {
		b.mu.Lock()
		still := b.st.Creds != nil && b.st.Creds.Token == creds.Token
		cursor := b.st.Cursor
		b.mu.Unlock()
		if !still {
			return
		}
		p, err := c.GetUpdates(ctx, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			switch {
			case ilink.CodeOf(err) == ilink.CodeReplaced:
				b.unbind(ctx, creds.Token)
				b.setStatus(connector.Status{State: connector.StateNotSignedIn, Error: i18n.T(b.lang(), "wechat-wechat.revoked")})
				return
			case !ilink.Retryable(err):
				b.setStatus(connector.Status{State: connector.StateError, Account: ok.Account, Error: i18n.T(b.lang(), "wechat-wechat.refused", i18n.Args{"error": err.Error()})})
			default:
				b.setStatus(connector.Status{State: connector.StateError, Account: ok.Account, Error: i18n.T(b.lang(), "wechat-wechat.retrying", i18n.Args{"error": err.Error()})})
			}
			wait := backoff[min(failures, len(backoff)-1)]
			failures++
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		if failures > 0 {
			b.setStatus(ok)
			failures = 0
		}
		for _, m := range p.Messages {
			b.handle(ctx, c, creds, m)
		}
		b.mu.Lock()
		if b.st.Creds != nil && b.st.Creds.Token == creds.Token && b.st.Cursor != p.Cursor {
			b.st.Cursor = p.Cursor
			b.saveLocked(ctx)
		}
		b.mu.Unlock()
	}
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = c.NotifyStop(stop)
}

// unbind forgets the bound token (only if it is still token, when given), keeping it among the old ones.
func (b *Bot) unbind(ctx context.Context, token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.st.Creds == nil || token != "" && b.st.Creds.Token != token {
		return
	}
	b.st.Old = rememberToken(b.st.Old, b.st.Creds.Token)
	b.st.Creds, b.st.Cursor, b.st.Context = nil, "", ""
	b.typing = nil
	b.saveLocked(ctx)
}

func rememberToken(old []string, token string) []string {
	old = slices.DeleteFunc(old, func(t string) bool { return t == token })
	old = append(old, token)
	if len(old) > 10 {
		old = old[len(old)-10:]
	}
	return old
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
