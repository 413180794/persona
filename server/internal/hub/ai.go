package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/prompts"
)

// MailAccountGuess is what the AI reads out of pasted text (a provider's setup page, an IT email, notes…).
type MailAccountGuess struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Provider     string `json:"provider"` // gmail | feishu | imap
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	IMAPHost     string `json:"imap_host,omitempty"`
	IMAPPort     int    `json:"imap_port,omitempty"`
	SMTPHost     string `json:"smtp_host,omitempty"`
	SMTPPort     int    `json:"smtp_port,omitempty"`
	SMTPSecurity string `json:"smtp_security,omitempty"` // tls | starttls
}

func (h *Hub) aiRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/ai/mail-account", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		var out struct {
			MailAccountGuess
			Missing []string `json:"missing"`
			Note    string   `json:"note"`
		}
		if err := h.ai.JSON(ctx, prompts.RenderMailAccount(), in.Text, &out); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		g := out.MailAccountGuess
		g.Email = strings.ToLower(strings.TrimSpace(g.Email))
		if strings.HasSuffix(g.Email, "@gmail.com") || strings.HasSuffix(g.Email, "@googlemail.com") {
			g.Provider, g.Password = "gmail", strings.ReplaceAll(g.Password, " ", "")
		}
		if strings.Contains(g.IMAPHost, "larkoffice.com") || strings.Contains(g.IMAPHost, "feishu") {
			g.Provider = "feishu"
		}
		if g.Provider == "" {
			g.Provider = "imap"
		}
		if g.Name == "" {
			g.Name = g.Email
		}
		missing := []string{}
		if g.Email == "" {
			missing = append(missing, "email")
		}
		if g.Password == "" {
			missing = append(missing, "password")
		}
		writeJSON(w, map[string]any{"account": g, "missing": missing, "note": out.Note})
	})
}
