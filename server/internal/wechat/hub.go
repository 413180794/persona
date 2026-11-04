package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Hub calls the Persona hub's API with the address and token the hub gives a plugin that has hub_api
// (PERSONA_HUB_URL / PERSONA_HUB_TOKEN, docs/plugins.md「权限」).
type Hub struct {
	url, token string
	http       *http.Client
}

func NewHub(base, token string) *Hub {
	return &Hub{url: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// errGone is an answer of 410: what was asked about is over (an interaction answered somewhere else).
var errGone = errors.New("gone")

func (h *Hub) call(ctx context.Context, method, path string, body, out any) error {
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
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if res.StatusCode == http.StatusGone {
		return errGone
	}
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

func sessionPath(machine, session string) string {
	return "/api/v1/sessions/" + url.PathEscape(machine) + "/" + url.PathEscape(session)
}

// Assistant is where the Persona tab's assistant runs and its main conversation ("" until there is one).
func (h *Hub) Assistant(ctx context.Context) (machine, main string, err error) {
	var out struct {
		MachineID string `json:"machine_id"`
		Main      string `json:"main"`
	}
	err = h.call(ctx, "GET", "/api/v1/assistant", nil, &out)
	return out.MachineID, out.Main, err
}

// Image is a picture sent with a message.
type Image struct {
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

// Start starts an assistant conversation with text as its first message; main makes it the main conversation.
func (h *Hub) Start(ctx context.Context, text string, images []Image, main bool) (proto.Session, error) {
	var out struct {
		Session proto.Session `json:"session"`
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute) // the hub waits up to 110 s for the conversation to start
	defer cancel()
	hc := *h
	hc.http = &http.Client{}
	body := map[string]any{"prompt": text, "main": main}
	if len(images) > 0 {
		body["images"] = images
	}
	err := hc.call(ctx, "POST", "/api/v1/assistant/conversations", body, &out)
	return out.Session, err
}

// Send sends text to a session, once per clientMsgID (the hub drops a second send with the same id).
func (h *Hub) Send(ctx context.Context, machine, session, text string, images []Image, clientMsgID string) error {
	body := map[string]any{"text": text, "client_msg_id": clientMsgID}
	if len(images) > 0 {
		body["images"] = images
	}
	return h.call(ctx, "POST", sessionPath(machine, session)+"/messages", body, nil)
}

// Interrupt stops the session's turn (Esc).
func (h *Hub) Interrupt(ctx context.Context, machine, session string) error {
	return h.call(ctx, "POST", sessionPath(machine, session)+"/interrupt", map[string]any{}, nil)
}

// Session is one session as the hub lists it; nil when it is not there.
func (h *Hub) Session(ctx context.Context, machine, id string) (*proto.Session, error) {
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

// LastSeq is the session's last item's sequence number (0 when it has none).
func (h *Hub) LastSeq(ctx context.Context, machine, session string) (int, error) {
	var out struct {
		ToSeq int          `json:"to_seq"`
		Items []proto.Item `json:"items"`
	}
	if err := h.call(ctx, "GET", sessionPath(machine, session)+"/items?limit=1&lazy_images=1", nil, &out); err != nil {
		return 0, err
	}
	if out.ToSeq == 0 && len(out.Items) > 0 {
		out.ToSeq = out.Items[len(out.Items)-1].Seq
	}
	return out.ToSeq, nil
}

// ItemsAfter are the session's items after seq, all of them (page by page).
func (h *Hub) ItemsAfter(ctx context.Context, machine, session string, seq int) ([]proto.Item, error) {
	var all []proto.Item
	for range 40 {
		var out struct {
			Items     []proto.Item `json:"items"`
			MoreAfter bool         `json:"more_after"`
			ToSeq     int          `json:"to_seq"`
		}
		path := sessionPath(machine, session) + "/items?lazy_images=1&after_seq=" + strconv.Itoa(seq)
		if err := h.call(ctx, "GET", path, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Items...)
		next := out.ToSeq
		if len(out.Items) > 0 {
			next = max(next, out.Items[len(out.Items)-1].Seq)
		}
		if !out.MoreAfter || next <= seq {
			break
		}
		seq = next
	}
	return all, nil
}

// FullText is the whole text of an item cut for pages (Item.FullSize > 0).
func (h *Hub) FullText(ctx context.Context, machine, session, item string) (string, error) {
	var out proto.Item
	err := h.call(ctx, "GET", sessionPath(machine, session)+"/items/"+url.PathEscape(item)+"/full", nil, &out)
	return out.Text, err
}

// Interactions are the cards waiting for an answer.
func (h *Hub) Interactions(ctx context.Context) ([]proto.Interaction, error) {
	var out struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	err := h.call(ctx, "GET", "/api/v1/interactions", nil, &out)
	return out.Interactions, err
}

// Resolve answers a card; errGone when it was answered somewhere else (or is gone).
func (h *Hub) Resolve(ctx context.Context, id string, r proto.Resolution) error {
	return h.call(ctx, "POST", "/api/v1/interactions/"+url.PathEscape(id)+"/resolve", r, nil)
}
