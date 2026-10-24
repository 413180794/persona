// Package ai calls an Anthropic-compatible Messages API for small structured tasks in the app (e.g. turning a
// pasted blob into a mail account). It reuses the gateway the cc_ds launcher uses on example-machine; the key is read from
// its key file at call time and never stored by Jarvis.
package ai

import (
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
)

type Config struct {
	BaseURL    string `json:"base_url"`
	APIKeyFile string `json:"api_key_file"`
	Model      string `json:"model"`
}

// Defaults mirror the cc_ds launcher on example-machine (relay-history → DeepSeek).
func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = "http://127.0.0.1:18000"
	}
	if c.APIKeyFile == "" {
		c.APIKeyFile = "~/.claude-relay-history/ds-chain-api-key"
	}
	if c.Model == "" {
		c.Model = "deepseek/v4.1-flash-chain"
	}
	return c
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg.withDefaults(), http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) key() (string, error) {
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
	key, err := c.key()
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{
		"model": c.cfg.Model, "max_tokens": maxTokens, "system": system,
		"messages": []any{map[string]any{"role": "user", "content": user}},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("ai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("ai: decode: %w", err)
	}
	var b strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String(), nil
}

// JSON asks for a JSON object and decodes the first one in the reply into out.
func (c *Client) JSON(ctx context.Context, system, user string, out any) error {
	text, err := c.Complete(ctx, system+"\n\nReply with a single JSON object only, no prose, no code fences.", user, 1024)
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
