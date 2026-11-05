package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector/github"
)

// Where GitHub's token comes from, as the user picks it in 设置 › 连接器 › GitHub (docs/connectors.md「二 · 7」): a
// computer's gh login or the device flow. The choice is kept in settings ("github_auth"); hub.json's
// github.auth.from_machine is only the default until the user chooses.

const githubAuthKey = "github_auth"

// githubAuth is the user's choice.
type githubAuth struct {
	Source  string `json:"source"`            // github.SourceMachine | github.SourceDevice
	Machine string `json:"machine,omitempty"` // the machine's id, for SourceMachine
}

// githubMachine is a computer GitHub's token can come from, with its gh login as GitHub sees it.
type githubMachine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	github.MachineLogin
}

// githubAuthView is GET /api/v1/connectors/github/auth: the choice, the computers to choose from, and what the token
// in use may do (scopes are known once GitHub said; missing lists what the connector needs and does not have).
type githubAuthView struct {
	Source      string                 `json:"source"`
	Machine     string                 `json:"machine,omitempty"`
	Machines    []githubMachine        `json:"machines"`
	Scopes      []string               `json:"scopes,omitempty"`
	ScopesKnown bool                   `json:"scopes_known"`
	Missing     []github.RequiredScope `json:"missing,omitempty"`
}

func (h *Hub) githubConnector() *github.Connector {
	c, _ := h.connectors.Get("github").(*github.Connector)
	return c
}

// restoreGitHubSource applies the user's choice, kept from before, at start.
func (h *Hub) restoreGitHubSource(ctx context.Context) {
	c := h.githubConnector()
	if c == nil || h.store == nil {
		return
	}
	var a githubAuth
	if ok, err := h.store.Setting(ctx, githubAuthKey, &a); err != nil || !ok {
		return
	}
	h.applyGitHubSource(ctx, c, a)
}

func (h *Hub) applyGitHubSource(ctx context.Context, c *github.Connector, a githubAuth) {
	if a.Source == github.SourceMachine && a.Machine != "" {
		c.UseMachine(ctx, a.Machine, h.machineName(a.Machine), h.ghToken(a.Machine))
		return
	}
	c.UseDevice()
}

// githubAuthNow is what the page shows: every machine's gh login, asked at once (a few seconds at most).
func (h *Hub) githubAuthNow(ctx context.Context, c *github.Connector) githubAuthView {
	v := githubAuthView{Source: c.Source(), Machine: c.MachineID(), Machines: []githubMachine{}}
	v.Scopes, v.ScopesKnown = c.Scopes()
	if v.ScopesKnown {
		v.Missing = github.MissingScopes(v.Scopes)
	}
	ms := h.machinesNow()
	out := make([]githubMachine, len(ms))
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, m := range ms {
		out[i] = githubMachine{ID: m.cfg.ID, Name: m.cfg.Name}
		if out[i].Name == "" {
			out[i].Name = m.cfg.ID
		}
		if !m.isOnline() {
			out[i].State = "offline"
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].MachineLogin = c.Probe(ctx, h.ghToken(m.cfg.ID))
		}()
	}
	wg.Wait()
	v.Machines = out
	return v
}

var errNoGitHub = errors.New("no GitHub connector")

func (h *Hub) githubAuthRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/connectors/github/auth", func(w http.ResponseWriter, r *http.Request) {
		c := h.githubConnector()
		if c == nil {
			writeErr(w, http.StatusNotFound, errNoGitHub)
			return
		}
		writeJSON(w, h.githubAuthNow(r.Context(), c))
	})
	// Choose where the token comes from: {"source": "machine", "machine": "<id>"} or {"source": "device"}. Choosing
	// one leaves the other (the device flow's token is forgotten when a computer is chosen).
	api.HandleFunc("PUT /api/v1/connectors/github/auth", func(w http.ResponseWriter, r *http.Request) {
		c := h.githubConnector()
		if c == nil {
			writeErr(w, http.StatusNotFound, errNoGitHub)
			return
		}
		var a githubAuth
		if err := decodeJSONBody(w, r, &a, 4<<10); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		switch {
		case a.Source == github.SourceDevice:
			a.Machine = ""
		case a.Source == github.SourceMachine && h.localMachine(a.Machine) != nil:
		default:
			writeErr(w, http.StatusBadRequest, fmt.Errorf("source %q / machine %q", a.Source, a.Machine))
			return
		}
		if h.store != nil {
			if err := h.store.PutSetting(r.Context(), githubAuthKey, a); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		h.applyGitHubSource(r.Context(), c, a)
		writeJSON(w, h.githubAuthNow(r.Context(), c))
	})
}
