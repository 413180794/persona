package proc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The connector's own settings, its inbox's settling, the computers it depends on and where its sign-in comes from:
// what the hub's own GitHub connector does in process, for a program that takes its place (connector.
// MigratableConnectors). Each goes to the program only when its manifest declares it (connector.As).

// Options are the settings as the program last gave them (or as the hub last set them, before it ran).
func (c *Conn) Options() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.options
}

// SetOptions gives the program the user's settings; one not running gets them when it starts (syncOptions). A run
// ending under the call counts as not running: the settings stay pending for the next one instead of being lost.
func (c *Conn) SetOptions(raw json.RawMessage) error {
	ctx := c.life()
	err := c.call(ctx, serve.MOptionsSet, serve.OptionsParams{Options: raw}, nil)
	if errors.Is(err, errNotRunning) || (err != nil && ctx.Err() != nil) {
		c.mu.Lock()
		c.options, c.sent = raw, false
		c.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	// The program took raw: keep it until it says what it kept, so a failed read neither stores nor re-sends the
	// settings it had before.
	c.mu.Lock()
	c.options = raw
	c.mu.Unlock()
	c.readOptions(ctx, func(method string, params, out any) error {
		return c.call(ctx, method, params, out)
	})
	c.mu.Lock()
	c.sent = true
	c.mu.Unlock()
	return nil
}

// syncOptions, right after the program (re)started, gives it the user's choice of sign-in (SetAuth) and the settings
// the hub set, then reads the settings it keeps. When the program does not take either, nothing is marked sent and
// the hub's settings stay pending, so the next refresh tries again instead of keeping the program's defaults.
func (c *Conn) syncOptions(ctx context.Context, call func(method string, params, out any) error) {
	c.mu.Lock()
	pending, sent, auth := c.options, c.sent, c.auth
	c.mu.Unlock()
	if sent {
		return
	}
	if auth != nil {
		if err := call(serve.MAuthSet, auth, nil); err != nil {
			c.cfg.Logf("connector %s: sign-in source: %v", c.cfg.ID, err)
			return
		}
	}
	if !c.Manifest().Has(connector.CapOptions) {
		c.mu.Lock()
		c.sent = true
		c.mu.Unlock()
		return
	}
	if len(pending) > 0 {
		if err := call(serve.MOptionsSet, serve.OptionsParams{Options: pending}, nil); err != nil {
			c.cfg.Logf("connector %s: settings: %v", c.cfg.ID, err)
			return
		}
	}
	c.readOptions(ctx, call)
	c.mu.Lock()
	c.sent = true
	c.mu.Unlock()
}

func (c *Conn) readOptions(_ context.Context, call func(method string, params, out any) error) {
	var out serve.OptionsResult
	if err := call(serve.MOptionsGet, struct{}{}, &out); err != nil {
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != serve.CodeNoCapability {
			c.cfg.Logf("connector %s: read settings: %v", c.cfg.ID, err)
		}
		return
	}
	c.mu.Lock()
	c.options = out.Options
	c.mu.Unlock()
}

// Settle asks the program which of the open items' threads are over; none when it cannot answer.
func (c *Conn) Settle(ctx context.Context, threads []string) map[string]string {
	var out serve.SettleResult
	if err := c.call(ctx, serve.MSettle, serve.SettleParams{Threads: threads}, &out); err != nil {
		c.cfg.Logf("connector %s: settle: %v", c.cfg.ID, err)
		return map[string]string{}
	}
	if out.Settled == nil {
		return map[string]string{}
	}
	return out.Settled
}

// MachineOnline tells the program a computer connected (in the background: the hub does not wait). The call ends
// with the connector's run; a refusal is logged, as the program then never hears the computer is back.
func (c *Conn) MachineOnline(id string) {
	ctx := c.life()
	go func() {
		err := c.call(ctx, serve.MMachineOnline, serve.MachineParams{Machine: id}, nil)
		if err == nil || errors.Is(err, errNotRunning) || ctx.Err() != nil {
			return
		}
		if re := (*rpc.Error)(nil); errors.As(err, &re) && re.Code == serve.CodeNoCapability {
			return
		}
		c.cfg.Logf("connector %s: %s %s: %v", c.cfg.ID, serve.MMachineOnline, id, err)
	}()
}

// Auth is where the program's sign-in comes from (auth.get).
func (c *Conn) Auth(ctx context.Context) (serve.AuthView, error) {
	var v serve.AuthView
	err := c.call(ctx, serve.MAuthGet, struct{}{}, &v)
	return v, err
}

// SetAuth gives the program the user's choice of where its sign-in comes from (auth.set), and again at each start;
// one not running gets it when it starts (the view is then the choice alone).
func (c *Conn) SetAuth(ctx context.Context, a serve.AuthSource) (serve.AuthView, error) {
	c.mu.Lock()
	c.auth = &a
	c.mu.Unlock()
	var v serve.AuthView
	err := c.call(ctx, serve.MAuthSet, a, &v)
	if errors.Is(err, errNotRunning) {
		return serve.AuthView{Source: a.Source, Machine: a.Machine}, nil
	}
	return v, err
}

// ghToken answers credentials.gh_token through Config.GhToken; the token goes to the program only, never a log.
func (c *Conn) ghToken(ctx context.Context, params json.RawMessage) (any, error) {
	if c.cfg.GhToken == nil {
		return nil, &rpc.Error{Code: serve.CodeNotPermitted, Message: "this extension was not granted gh_login"}
	}
	var p serve.GhTokenParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	tok, err := c.cfg.GhToken(ctx, p.Machine, p.Host)
	if err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			return nil, re
		}
		return nil, &rpc.Error{Message: err.Error()}
	}
	return serve.GhTokenResult{Token: tok}, nil
}
