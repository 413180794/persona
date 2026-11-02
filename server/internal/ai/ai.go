// Package ai calls an Anthropic-compatible Messages API, or an OpenAI-compatible Responses API, for small structured
// tasks in the app (e.g. turning a pasted blob into a mail account). The key is read from its key file at call time
// and never stored by Jarvis.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/prompts"
)

type Config struct {
	// API is "messages" (Anthropic Messages, the default) or "responses" (OpenAI Responses, streamed).
	API        string `json:"api"`
	BaseURL    string `json:"base_url"`
	APIKeyFile string `json:"api_key_file"`
	// APIKey is an in-memory credential supplied by the hub's encrypted provider store.
	APIKey string `json:"-"`
	Model  string `json:"model"`
	// ReasoningEffort is sent as reasoning.effort to the Responses API when set (e.g. "low").
	ReasoningEffort string `json:"reasoning_effort"`
}

// Over is c with its empty fields taken from base: a task that uses another model names only what differs.
func (c Config) Over(base Config) Config {
	if c.API == "" {
		c.API = base.API
	}
	if c.BaseURL == "" {
		c.BaseURL = base.BaseURL
	}
	if c.APIKeyFile == "" {
		c.APIKeyFile = base.APIKeyFile
	}
	if c.Model == "" {
		c.Model = base.Model
	}
	if c.ReasoningEffort == "" {
		c.ReasoningEffort = base.ReasoningEffort
	}
	return c
}

// configured: there is a model to call. Nothing is assumed (it used to default to one person's gateway and key file):
// without hub.json's "ai" the helpers that need a model say so.
func (c Config) configured() bool {
	return c.BaseURL != "" && c.Model != "" && (c.APIKeyFile != "" || c.APIKey != "")
}

// errNotConfigured is shown in the app as is (e.g. under AI 草拟回复), so it says where to fix it there.
var errNotConfigured = errors.New("还没有能直接调用的模型：在 设置 › 模型供应商 添加带 Key 的接入，再到 设置 › 模型配置 为这项选择它" +
	`（或在 hub.json 写 "ai"）`)

type Client struct {
	cfg     Config
	http    *http.Client
	resolve func(context.Context) (*Client, error)
}

// NewResolved resolves the execution configuration at the start of every request.
func NewResolved(resolve func(context.Context) (*Client, error)) *Client {
	return &Client{resolve: resolve}
}

// Config returns the immutable connection configuration, never an API response.
func (c *Client) Config() Config { return c.cfg }

func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) key() (string, error) {
	if c.cfg.APIKey != "" {
		return c.cfg.APIKey, nil
	}
	path := c.cfg.APIKeyFile
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("ai: read api key: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Complete sends one user message with a system prompt and returns the text reply.
func (c *Client) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if c.resolve != nil {
		resolved, err := c.resolve(ctx)
		if err != nil {
			return "", err
		}
		return resolved.Complete(ctx, system, user, maxTokens)
	}
	if !c.cfg.configured() {
		return "", errNotConfigured
	}
	key, err := c.key()
	if err != nil {
		return "", err
	}
	// Every client of one address and key goes through the same gate, which holds requests back while the upstream
	// is out of quota or busy (limit.go).
	g := gateFor(c.cfg.BaseURL, key)
	probe, err := g.admit(ctx)
	if err != nil {
		return "", err
	}
	var text string
	var answered bool
	if c.cfg.API == "responses" {
		text, answered, err = c.respond(ctx, key, system, user)
	} else {
		text, answered, err = c.messages(ctx, key, system, user, maxTokens)
	}
	return text, g.done(probe, answered, err)
}

// messages calls the Messages API; answered is whether the upstream said 200.
func (c *Client) messages(ctx context.Context, key, system, user string, maxTokens int) (text string, answered bool, err error) {
	body, _ := json.Marshal(map[string]any{
		"model": c.cfg.Model, "max_tokens": maxTokens, "system": system,
		"messages": []any{map[string]any{"role": "user", "content": user}},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("ai: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return "", false, newHTTPError(resp, raw)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", true, fmt.Errorf("ai: decode: %w", err)
	}
	var b strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String(), true, nil
}

// respond calls the Responses API. It always streams: some compatible endpoints (Codex-backed relays) reject
// stream=false, and the text is just the concatenated output_text deltas. The system prompt goes in as a developer
// message rather than `instructions`, which those relays replace with their own.
func (c *Client) respond(ctx context.Context, key, system, user string) (text string, answered bool, err error) {
	req := map[string]any{
		"model": c.cfg.Model, "store": false, "stream": true,
		"input": []any{
			map[string]any{"role": "developer", "content": system},
			map[string]any{"role": "user", "content": user},
		},
	}
	if c.cfg.ReasoningEffort != "" {
		req["reasoning"] = map[string]any{"effort": c.cfg.ReasoningEffort}
	}
	body, _ := json.Marshal(req)
	hreq, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	hreq.Header.Set("content-type", "application/json")
	hreq.Header.Set("accept", "text/event-stream")
	hreq.Header.Set("authorization", "Bearer "+key)
	resp, err := c.http.Do(hreq)
	if err != nil {
		return "", false, fmt.Errorf("ai: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", false, newHTTPError(resp, raw)
	}
	var b strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Message  string `json:"message"`
			Response struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			b.WriteString(ev.Delta)
		case "response.completed":
			return b.String(), true, nil
		case "response.failed", "response.incomplete", "error":
			msg := ev.Message
			if ev.Response.Error != nil {
				msg = ev.Response.Error.Message
			}
			return "", true, fmt.Errorf("ai: %s: %s", ev.Type, msg)
		}
	}
	if err := sc.Err(); err != nil {
		return "", true, fmt.Errorf("ai: read stream: %w", err)
	}
	if b.Len() == 0 {
		return "", true, errors.New("ai: stream ended without a reply")
	}
	return b.String(), true, nil
}

// JSON asks for a JSON object and decodes the first one in the reply into out.
func (c *Client) JSON(ctx context.Context, system, user string, out any) error {
	return c.JSONMax(ctx, system, user, 1024, out)
}

// JSONMax is JSON with room for a longer answer (summaries).
func (c *Client) JSONMax(ctx context.Context, system, user string, maxTokens int, out any) error {
	text, err := c.Complete(ctx, prompts.RenderJSONOnly(system), user, maxTokens)
	if err != nil {
		return err
	}
	obj, err := FirstObject(text)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(obj), out)
}

// FirstObject extracts the first balanced {...} from model output (tolerating fences and chatter).
func FirstObject(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", errors.New("ai: no JSON object in reply")
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case ch == '\\' && inStr:
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", errors.New("ai: unterminated JSON object in reply")
}
