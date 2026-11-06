package extmgr

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/connector"
)

// auditStore keeps the audit (plugin_audit): who did what and when, never a secret or a value someone typed.
type auditStore interface {
	AddExtensionAudit(ctx context.Context, id, action, actor string, detail json.RawMessage) error
}

// What someone did to a connector, as plugin_audit records it: the plugin column holds the connector's id, and the
// actions carry a connector_ prefix so they never read as the extension's own (install, auth_signed_in, …) (#5281).
const (
	auditConnectorLoginStart     = "connector_login_start"     // a sign-in started (an empty step)
	auditConnectorSignedIn       = "connector_signed_in"       // {account}: the sign-in is done
	auditConnectorLogout         = "connector_logout"          // signed out: the credential is gone
	auditConnectorEnabled        = "connector_enabled"         // {enabled}: switched on or off
	auditConnectorFeed           = "connector_feed"            // {feed, on}: "Add to knowledge base" / "Add to Inbox"
	auditConnectorOptions        = "connector_options"         // its own settings changed (never their values)
	auditConnectorDataDeleted    = "connector_data_deleted"    // {chats, people, inbox}: "Delete saved data"
	auditConnectorArchiveDeleted = "connector_archive_deleted" // {chats, people}: "Delete saved knowledge base content"
	auditConnectorAuthSource     = "connector_auth_source"     // {source, machine}: where GitHub's token comes from
)

// auditConnector records an action on connector id by whoever made the request (actorOf: device:<id>, or the hub
// token). detail holds only what is safe to keep: never a credential, a value someone typed or a setting's value. A
// record that cannot be written does not undo the action, which already happened; it is logged.
func (h *Manager) auditConnector(ctx context.Context, id, action string, detail map[string]any) {
	st, _ := h.store.(auditStore)
	if st == nil {
		return
	}
	raw := json.RawMessage(`{}`)
	if len(detail) > 0 {
		if b, err := json.Marshal(detail); err == nil {
			raw = b
		}
	}
	// The action is done even when the app stopped waiting for the answer: its record is kept all the same.
	if err := st.AddExtensionAudit(context.WithoutCancel(ctx), id, action, actorOf(ctx), raw); err != nil {
		h.logf("connector %s: audit %s: %v", id, action, err)
	}
}

// connectorLoginStep takes a step of signing in to connector id (Settings › Connectors, a channel's binding) and
// records the start (an empty step) and the account once it is done, as an extension's own sign-in is recorded. The
// values the user typed go to the connector only.
func (h *Manager) connectorLoginStep(ctx context.Context, id string, l connector.Login, step string, values map[string]string) (connector.Prompt, error) {
	p, err := l.LoginStep(ctx, step, values)
	if err != nil {
		return p, err
	}
	if step == "" {
		h.auditConnector(ctx, id, auditConnectorLoginStart, nil)
	}
	if p.Done {
		var detail map[string]any
		if p.Account != "" {
			detail = map[string]any{"account": p.Account}
		}
		h.auditConnector(ctx, id, auditConnectorSignedIn, detail)
	}
	return p, nil
}

// connectorLogout signs out of connector id and records it.
func (h *Manager) connectorLogout(ctx context.Context, id string, l connector.Login) error {
	if err := l.Logout(ctx); err != nil {
		return err
	}
	h.auditConnector(ctx, id, auditConnectorLogout, nil)
	return nil
}

// optionsOf is connector id's own settings as they are now (connector.Options), nil when it has none.
func (h *Manager) optionsOf(id string) json.RawMessage {
	o, ok := connector.As[connector.Options](h.connectors.Get(id))
	if !ok {
		return nil
	}
	raw := o.Options()
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return raw
	}
	return out.Bytes()
}

// forgotten is a deletion's record: what went, and "incomplete" when part of it failed (the hub's log says why).
func forgotten(counts map[string]any, err error) map[string]any {
	if err != nil {
		counts["incomplete"] = true
	}
	return counts
}
