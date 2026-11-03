package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// Signing in from the app with GitHub's device flow (docs/connectors.md「二 · 7」): the user registers their own OAuth
// App (Enable Device Flow) and gives its client ID once; GitHub shows a code to enter at github.com/login/device; the
// token it then grants is kept sealed in the hub (connector.Secrets) and used before a computer's gh login. The
// client ID is the user's, never the program's.

// deviceScopes are what the connector uses (source.go RequiredScopes): repo covers reading private repositories'
// PRs, issues, comments, checks and CI logs, searching code and issues, commenting and reviewing, and notifications.
const deviceScopes = "repo"

// deviceLogin is what is kept in the hub's secrets for GitHub.
type deviceLogin struct {
	ClientID string `json:"client_id,omitempty"`
	Token    string `json:"token,omitempty"`
}

// devicePending is a device code waiting for the user.
type devicePending struct {
	code, userCode, uri string
	interval            int
	expires             time.Time
}

// UseSecrets gives the connector the hub's store for its sign-in (the device flow's client ID and token).
func (c *Connector) UseSecrets(s connector.Secrets) {
	c.mu.Lock()
	c.secrets = s
	c.mu.Unlock()
}

// deviceState is what the secrets keep, read once.
func (c *Connector) deviceState(ctx context.Context) deviceLogin {
	c.mu.Lock()
	s, loaded, d := c.secrets, c.devLoaded, c.dev
	c.mu.Unlock()
	if loaded || s == nil {
		return d
	}
	raw, err := s.Load(ctx)
	if err != nil {
		return d
	}
	_ = json.Unmarshal(raw, &d)
	c.mu.Lock()
	c.dev, c.devLoaded = d, true
	c.mu.Unlock()
	return d
}

func (c *Connector) saveDevice(ctx context.Context, d deviceLogin) error {
	c.mu.Lock()
	s := c.secrets
	c.dev, c.devLoaded = d, true
	c.mu.Unlock()
	if s == nil {
		return errors.New("no secrets store")
	}
	raw, _ := json.Marshal(d)
	return s.Save(ctx, raw)
}

// viaDevice says whether the token in use is the device flow's.
func (c *Connector) viaDevice() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dev.Token != ""
}

// clientIDStep asks for the user's OAuth App's client ID.
func clientIDStep(err string) connector.Prompt {
	return connector.Prompt{Step: "client_id", Error: err,
		Fields: []connector.Field{{Name: "client_id", Label: "Client ID", Placeholder: "你的 OAuth App 的 Client ID"}},
		Hint:   "在 GitHub 的 Settings › Developer settings › OAuth Apps 注册一个 OAuth App，勾上 Enable Device Flow，把它的 Client ID 填在这里（不用 client secret）。"}
}

func (p devicePending) prompt(err string) connector.Prompt {
	return connector.Prompt{Step: "device", Kind: connector.PromptDeviceCode, URL: p.uri, UserCode: p.userCode, Interval: p.interval, Error: err,
		Hint: "在 GitHub 的验证页输入这个验证码并授权；授权后这里会自动继续。"}
}

// LoginStep signs in with the device flow: the client ID once, then the code, then waiting for the user.
func (c *Connector) LoginStep(ctx context.Context, step string, values map[string]string) (connector.Prompt, error) {
	d := c.deviceState(ctx)
	switch step {
	case "":
		if d.ClientID == "" {
			return clientIDStep(""), nil
		}
		return c.startDevice(ctx, d.ClientID)
	case "client_id":
		id := strings.TrimSpace(values["client_id"])
		if id == "" || strings.ContainsAny(id, " /") {
			return clientIDStep("Client ID 不对：从 OAuth App 的页面原样复制过来"), nil
		}
		d.ClientID = id
		if err := c.saveDevice(ctx, d); err != nil {
			return connector.Prompt{}, err
		}
		return c.startDevice(ctx, id)
	case "device":
		if c.Source() != SourceDevice {
			return connector.Prompt{Step: "device", Error: "先选「设备码登录」"}, nil
		}
		return c.pollDevice(ctx, d)
	}
	return connector.Prompt{}, fmt.Errorf("unknown step %q", step)
}

// startDevice asks GitHub for a code for the user to enter.
func (c *Connector) startDevice(ctx context.Context, clientID string) (connector.Prompt, error) {
	var r struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
	}
	if err := c.postForm(ctx, "/login/device/code", url.Values{"client_id": {clientID}, "scope": {deviceScopes}}, &r); err != nil {
		return connector.Prompt{}, err
	}
	if r.DeviceCode == "" {
		why := "GitHub 不认这个 Client ID，或者这个 OAuth App 没有勾上 Enable Device Flow"
		if r.Error != "" && r.Error != "unauthorized_client" && r.Error != "device_flow_disabled" {
			why = "GitHub 没给验证码（" + r.Error + "）"
		}
		return clientIDStep(why), nil
	}
	p := devicePending{code: r.DeviceCode, userCode: r.UserCode, uri: r.VerificationURI, interval: max(r.Interval, 5),
		expires: time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)}
	c.mu.Lock()
	c.pending = &p
	c.mu.Unlock()
	return p.prompt(""), nil
}

// pollDevice asks GitHub once whether the user has entered the code; the same step while not yet.
func (c *Connector) pollDevice(ctx context.Context, d deviceLogin) (connector.Prompt, error) {
	c.mu.Lock()
	p := c.pending
	c.mu.Unlock()
	if p == nil || d.ClientID == "" {
		return c.LoginStep(ctx, "", nil)
	}
	if time.Now().After(p.expires) {
		return c.restartDevice(ctx, d.ClientID, "验证码过期了，换了一个新的")
	}
	var r struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Interval    int    `json:"interval"`
	}
	err := c.postForm(ctx, "/login/oauth/access_token", url.Values{"client_id": {d.ClientID}, "device_code": {p.code},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &r)
	if err != nil {
		return connector.Prompt{}, err
	}
	switch {
	case r.AccessToken != "":
	case r.Error == "authorization_pending":
		return p.prompt(""), nil
	case r.Error == "slow_down":
		c.mu.Lock()
		p.interval = max(r.Interval, p.interval+5)
		c.mu.Unlock()
		return p.prompt(""), nil
	case r.Error == "expired_token":
		return c.restartDevice(ctx, d.ClientID, "验证码过期了，换了一个新的")
	case r.Error == "access_denied":
		return c.restartDevice(ctx, d.ClientID, "在 GitHub 上取消了授权")
	default:
		return c.restartDevice(ctx, d.ClientID, "GitHub 没有授权（"+r.Error+"）")
	}
	d.Token = r.AccessToken
	if err := c.saveDevice(ctx, d); err != nil {
		return connector.Prompt{}, err
	}
	c.mu.Lock()
	c.token, c.pending = d.Token, nil
	c.mu.Unlock()
	c.check(ctx)
	st := c.Status(ctx)
	if st.State != connector.StateOK {
		return connector.Prompt{Step: "device", Error: st.Error}, nil
	}
	return connector.Prompt{Done: true, Account: st.Account}, nil
}

func (c *Connector) restartDevice(ctx context.Context, clientID, why string) (connector.Prompt, error) {
	p, err := c.startDevice(ctx, clientID)
	if err == nil && p.Error == "" {
		p.Error = why
	}
	return p, err
}

// Logout forgets the device flow's token (the client ID stays, for the next sign-in). A computer's gh login is not
// the hub's to end: choosing another source leaves it (source.go).
func (c *Connector) Logout(ctx context.Context) error {
	if err := c.forgetDevice(ctx); err != nil {
		return err
	}
	c.check(ctx)
	return nil
}

func (c *Connector) forgetDevice(ctx context.Context) error {
	c.mu.Lock()
	if c.source == SourceDevice {
		c.token = ""
	}
	c.pending = nil
	c.mu.Unlock()
	d := c.deviceState(ctx)
	if d.Token == "" {
		return nil
	}
	d.Token = ""
	return c.saveDevice(ctx, d)
}

// postForm posts to GitHub's web host (the device flow is not the API's), answers as JSON.
func (c *Connector) postForm(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.web+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New(Scrub(err.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("GitHub %s: %d", path, resp.StatusCode)
	}
	return json.Unmarshal(raw, out)
}
