package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

const (
	pairLifetime = 10 * time.Minute
	pairGrace    = 2 * time.Minute
)

type pairCode struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
	HubID     string    `json:"hub_id"`
	FP        string    `json:"fp"`
	TLSPort   int       `json:"tls_port"`
	Alt       []string  `json:"alt"`
	// Machine: made for that machine's jarvis-link (`jarvisd hub pair --machine`); the URL is then jarvis://link?….
	Machine string `json:"machine,omitempty"`
	PairURL string `json:"pair_url"`
}

func (p pairCode) pairURL() string {
	values := url.Values{"url": {p.URL}, "token": {p.Token}}
	if p.Machine != "" {
		values.Set("machine", p.Machine)
	}
	if p.HubID != "" {
		values.Set("hub", p.HubID)
	}
	if p.FP != "" {
		values.Set("fp", p.FP)
	}
	if p.TLSPort > 0 {
		values.Set("tls", strconv.Itoa(p.TLSPort))
	}
	for _, alt := range p.Alt {
		values.Add("alt", alt)
	}
	if p.Machine != "" {
		return "jarvis://link?" + values.Encode()
	}
	return "jarvis://pair?" + values.Encode()
}

// pairRoutes are the pairing endpoints: short-lived pairing tokens for `jarvisd hub pair`
// (docs/redesign/hub-changes.md 1.3).
func (h *Hub) pairRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/auth/pair-codes", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).ID != "" || h.sourceOf(r) != sourceLoopback {
			writeErr(w, http.StatusForbidden, errors.New("pairing codes require the master token over loopback"))
			return
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		// {"machine": id} makes a code for that link machine's jarvis-link (link.go); no body, one for an app.
		var req struct {
			Machine string `json:"machine"`
		}
		if b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<10)); len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &req); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if req.Machine != "" {
			if m := h.cfgMachine(req.Machine); m == nil || m.cfg.Kind != kindLink {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("hub.json has no machine %q of kind link", req.Machine))
				return
			}
			if port, _ := h.tlsPin(); port == 0 {
				writeErr(w, http.StatusConflict, errors.New("Persona Link only uses TLS: set public.tls_listen in hub.json"))
				return
			}
		}
		var p *pairCode
		var err error
		if req.Machine == "" && h.isMember() {
			// An app pairs with the center: devices are the center's (plan-hub-center.md 3).
			p = &pairCode{}
			err = h.meshCall(r.Context(), h.centerID(), "pair.code", map[string]any{}, p)
		} else {
			p, err = h.newPairCode(r.Context(), req.Machine)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, p)
	})
}

// newPairCode makes a one-time pairing code: for an app (machine ""), or for that link machine's jarvis-link.
func (h *Hub) newPairCode(ctx context.Context, machine string) (*pairCode, error) {
	if h.store == nil {
		return nil, errors.New("no database")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, errors.New("could not generate pairing token")
	}
	p := pairCode{Token: "jp_" + hex.EncodeToString(secret[:]), ExpiresAt: time.Now().UTC().Add(pairLifetime),
		URL: h.cfg.PublicURL, HubID: h.hubID(), Alt: []string{}, Machine: machine}
	p.TLSPort, p.FP = h.tlsPin()
	for _, alt := range h.pairCandidates() {
		if len(p.Alt) == 2 {
			break
		}
		p.Alt = append(p.Alt, alt)
	}
	if err := h.store.CreatePairToken(ctx, hashToken(p.Token), p.ExpiresAt, p.Machine); err != nil {
		return nil, err
	}
	p.PairURL = p.pairURL()
	return &p, nil
}

func pairExpires(p *db.PairToken) time.Time {
	until := p.ExpiresAt
	if p.UsedAt != nil && p.UsedAt.Add(pairGrace).Before(until) {
		until = p.UsedAt.Add(pairGrace)
	}
	return until
}

// An open WS must not keep a pairing credential alive indefinitely. Recheck redemption while it is open,
// then cancel exactly at the earlier of its original expiry and the redemption grace deadline.
func (h *Hub) pairContext(parent context.Context, p principal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		defer cancel()
		until := p.PairExpires
		for {
			wait := min(time.Until(until), 15*time.Second)
			if wait <= 0 {
				return
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if !time.Now().Before(until) {
				return
			}
			lookup, stop := context.WithTimeout(ctx, 5*time.Second)
			pt, err := h.store.PairTokenByHash(lookup, p.PairHash)
			stop()
			if err != nil || pt == nil {
				return
			}
			until = pairExpires(pt)
		}
	}()
	return ctx, cancel
}

func (h *Hub) notifyPairing(name string) {
	if !h.push.ready() {
		h.logf("new device paired: %q (push unavailable)", name)
		return
	}
	h.push.send(map[string]any{"aps": map[string]any{
		"alert": map[string]any{"title": "Persona", "body": "有新设备配对：" + name},
		"sound": "default",
	}}, "", nil)
}
