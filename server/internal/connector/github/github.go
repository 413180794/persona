// Package github is the GitHub connector: the user's PRs, issues, reviews and checks, read with the token of a
// machine's gh login (docs/plan-connectors.md「四、GitHub 连接器」).
//
// The token lives only in this process's memory: it is never stored, logged, returned to a client, put in an error
// or handed to the assistant.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// Config is hub.json's "github".
type Config struct {
	Auth AuthConfig `json:"auth"`
}

// AuthConfig: whose gh login the hub reads, and for which host. Host is github.com when empty; a GitHub Enterprise
// host goes here and only here (not built yet: only github.com is tried).
type AuthConfig struct {
	FromMachine string `json:"from_machine,omitempty"`
	Host        string `json:"host,omitempty"`
}

// Configured says whether the hub should run the connector at all.
func (c *Config) Configured() bool { return c != nil && c.Auth.FromMachine != "" }

func (c *Config) host() string {
	if c.Auth.Host == "" {
		return "github.com"
	}
	return strings.ToLower(c.Auth.Host)
}

// Why a token could not be had (TokenSource's errors, errors.Is).
var (
	ErrNoMachine   = errors.New("github.auth.from_machine is not a machine in hub.json")
	ErrOffline     = errors.New("the machine is offline")
	ErrNodeTooOld  = errors.New("the machine's jarvisd is too old")
	ErrNoGh        = errors.New("gh is not installed")
	ErrNotSignedIn = errors.New("gh is not signed in")
)

// TokenSource asks the machine for its gh login's token for host.
type TokenSource func(ctx context.Context, host string) (string, error)

// Connector is GitHub. Run keeps its status (and token) fresh.
type Connector struct {
	cfg     Config
	machine string // the machine's name, for messages
	fetch   TokenSource
	api     string
	client  *http.Client

	mu     sync.Mutex
	token  string
	status connector.Status
	poke   chan struct{}
}

// New is the connector for cfg, reading the token of machine (its display name) through fetch.
func New(cfg Config, machine string, fetch TokenSource) *Connector {
	api := "https://api.github.com"
	if h := cfg.host(); h != "github.com" {
		api = "https://" + h + "/api/v3"
	}
	return &Connector{cfg: cfg, machine: machine, fetch: fetch, api: api, client: &http.Client{Timeout: 30 * time.Second},
		status: connector.Status{State: connector.StateNotSignedIn, Error: "正在读取 " + machine + " 上的 gh 登录…"}, poke: make(chan struct{}, 1)}
}

func (c *Connector) ID() string      { return "github" }
func (c *Connector) Name() string    { return "GitHub" }
func (c *Connector) Hosts() []string { return []string{c.cfg.host()} }

func (c *Connector) Status(context.Context) connector.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	st.Via = "来自 " + c.machine + " 的 gh 登录"
	return st
}

// MachineOnline: the machine the token comes from just connected, so a token that was missing may be there now.
func (c *Connector) MachineOnline(id string) {
	if id != c.cfg.Auth.FromMachine {
		return
	}
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

// How often Run checks again: when all is well, and when it is not.
var (
	checkOK    = 30 * time.Minute
	checkRetry = time.Minute
)

// Run checks the sign-in now and then until ctx ends: again soon while it fails, and when the machine connects.
func (c *Connector) Run(ctx context.Context) {
	for {
		c.check(ctx)
		wait := checkOK
		if c.Status(ctx).State != connector.StateOK {
			wait = checkRetry
		}
		select {
		case <-ctx.Done():
			return
		case <-c.poke:
		case <-time.After(wait):
		}
	}
}

// check asks GitHub who the token is (fetching one first if there is none) and records the answer.
func (c *Connector) check(ctx context.Context) {
	var user struct {
		Login string `json:"login"`
	}
	_, err := c.Get(ctx, "/user", &user)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.status = connector.Status{State: connector.StateOK, Account: user.Login, LastSync: time.Now().UTC(), LastEvent: c.status.LastEvent}
		return
	}
	c.status.State, c.status.Error = c.explain(err)
}

// explain turns an error into a state and one sentence for people.
func (c *Connector) explain(err error) (string, string) {
	m := c.machine
	switch {
	case errors.Is(err, ErrNoMachine):
		return connector.StateNotConfigured, "hub.json 的 github.auth.from_machine 不是 hub.json 里的机器"
	case errors.Is(err, ErrOffline):
		return connector.StateNotSignedIn, "GitHub 未授权（" + m + " 离线）：它连上后会自动再读"
	case errors.Is(err, ErrNodeTooOld):
		return connector.StateError, m + " 的 jarvisd 太旧，读不了 gh 登录：更新它（scripts/deploy.sh）"
	case errors.Is(err, ErrNoGh):
		return connector.StateNotSignedIn, m + " 上没有 gh：在那台机器上装好 gh 并运行 gh auth login"
	case errors.Is(err, ErrNotSignedIn):
		return connector.StateNotSignedIn, m + " 上的 gh 没有登录 " + c.cfg.host() + "，或者登录存在钥匙串里、经 ssh 读不到：" +
			"在那台机器上运行 gh auth login --insecure-storage"
	case errors.Is(err, errUnauthorized):
		return connector.StateNotSignedIn, m + " 上的 gh 登录失效了：在那台机器上运行 gh auth login"
	}
	return connector.StateError, "连不上 GitHub：" + Scrub(err.Error())
}

var errUnauthorized = errors.New("GitHub says the token is not valid (401)")

// currentToken is the token in memory, fetching one only when there is none or (refresh) GitHub refused it: a machine
// that went offline leaves the one in memory in use.
func (c *Connector) currentToken(ctx context.Context, refresh bool) (string, error) {
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" && !refresh {
		return tok, nil
	}
	fresh, err := c.fetch(ctx, c.cfg.host())
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token = fresh
	c.mu.Unlock()
	return fresh, nil
}

// Get reads path of the REST API into out (when not nil), refetching the token once if GitHub refuses it.
func (c *Connector) Get(ctx context.Context, path string, out any) (http.Header, error) {
	return c.request(ctx, http.MethodGet, path, nil, out)
}

func (c *Connector) request(ctx context.Context, method, path string, body []byte, out any) (http.Header, error) {
	tok, err := c.currentToken(ctx, false)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.send(ctx, method, path, body, tok)
		if err != nil {
			return nil, errors.New(Scrub(err.Error()))
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			// The machine may have signed in again since: ask it once more.
			if tok, err = c.currentToken(ctx, true); err != nil {
				return nil, err
			}
			continue
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return resp.Header, errUnauthorized
		case resp.StatusCode == http.StatusNotModified:
			return resp.Header, nil
		case resp.StatusCode >= 300:
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			return resp.Header, fmt.Errorf("GitHub %s %s: %d %s", method, path, resp.StatusCode, Scrub(e.Message))
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return resp.Header, fmt.Errorf("GitHub %s %s: %v", method, path, err)
			}
		}
		return resp.Header, nil
	}
}

func (c *Connector) send(ctx context.Context, method, path string, body []byte, tok string) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return c.client.Do(req)
}

var tokenLike = regexp.MustCompile(`\b(gh[opsur]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,}|[0-9a-f]{40})\b`)

// Scrub blanks anything that looks like a GitHub token in s (an error, a message): tokens never leave the hub.
func Scrub(s string) string { return tokenLike.ReplaceAllString(s, "[token]") }
