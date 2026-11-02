package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// hubClient calls the Jarvis hub's API with the credential the hub hands its plugins (JARVIS_HUB_URL / JARVIS_HUB_TOKEN).
type hubClient struct {
	url, token string
	http       *http.Client
}

func newHubClient(base, token string) *hubClient {
	return &hubClient{url: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (h *hubClient) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.url+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := h.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("%s %s: HTTP %d", method, path, res.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type kbHit struct {
	Path string `json:"path"`
	Line string `json:"line"`
}

// searchKnowledge: lines of the knowledge base matching every word of the query.
func (h *hubClient) searchKnowledge(ctx context.Context, query string) ([]kbHit, error) {
	var out struct {
		Hits []kbHit `json:"hits"`
	}
	err := h.call(ctx, "GET", "/api/v1/kb-search?q="+url.QueryEscape(query), nil, &out)
	return out.Hits, err
}

// agents: the coding agents on every machine, without the Jarvis tab's own conversations.
func (h *hubClient) agents(ctx context.Context) ([]proto.Session, error) {
	var out struct {
		Sessions []proto.Session `json:"sessions"`
	}
	if err := h.call(ctx, "GET", "/api/v1/sessions", nil, &out); err != nil {
		return nil, err
	}
	agents := out.Sessions[:0]
	for _, s := range out.Sessions {
		if !s.Assistant {
			agents = append(agents, s)
		}
	}
	return agents, nil
}

// session is one session as the hub lists it; nil when it is not there.
func (h *hubClient) session(ctx context.Context, machine, id string) (*proto.Session, error) {
	var out struct {
		Sessions []proto.Session `json:"sessions"`
	}
	if err := h.call(ctx, "GET", "/api/v1/sessions?machine_id="+url.QueryEscape(machine), nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Sessions {
		if out.Sessions[i].SessionID == id {
			return &out.Sessions[i], nil
		}
	}
	return nil, nil
}

func (h *hubClient) send(ctx context.Context, machine, session, text string) error {
	path := "/api/v1/sessions/" + url.PathEscape(machine) + "/" + url.PathEscape(session) + "/messages"
	return h.call(ctx, "POST", path, map[string]string{"text": text}, nil)
}

// assistant: where the Jarvis tab's assistant runs and its main conversation (empty until there is one).
func (h *hubClient) assistant(ctx context.Context) (machine, main string, err error) {
	var out struct {
		MachineID string `json:"machine_id"`
		Main      string `json:"main"`
	}
	err = h.call(ctx, "GET", "/api/v1/assistant", nil, &out)
	return out.MachineID, out.Main, err
}

// startMain starts an assistant conversation with prompt as its first message and makes it the main one.
func (h *hubClient) startMain(ctx context.Context, prompt string) (string, error) {
	var out struct {
		Session proto.Session `json:"session"`
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute) // the hub waits up to 110 s for the conversation to start
	defer cancel()
	client := *h.http
	client.Timeout = 0
	hc := *h
	hc.http = &client
	if err := hc.call(ctx, "POST", "/api/v1/assistant/conversations", map[string]any{"prompt": prompt, "main": true}, &out); err != nil {
		return "", err
	}
	return out.Session.SessionID, nil
}
