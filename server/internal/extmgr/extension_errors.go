package extmgr

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// extensionErrorCopy keeps opaque program/SDK diagnostics out of translated summaries. Known hub-owned messages
// are rendered from resources; the optional detail is diagnostic data, not authored interface copy.
func extensionErrorCopy(l i18n.Lang, err error, fallback string) (string, string) {
	var field *extension.FieldError
	if errors.As(err, &field) && field.Error() == err.Error() {
		return field.Text(l), ""
	}
	var msg *i18n.Message
	if errors.As(err, &msg) && msg.Error() == err.Error() {
		if msg.Key == "extension-status.not_found" {
			return i18n.T(l, "extension-status.missing_extension"), ""
		}
		if _, nested := msg.Args["err"]; !nested {
			return i18n.Localize(err, l), ""
		}
	}
	return i18n.ExtensionFailureText(l, err.Error(), fallback)
}

// Explicit language requests get translated summaries and separate diagnostics. No-locale legacy HTTP responses
// retain their original error text and codes; new fields never replace the machine-readable code.
func writeExtensionResponseError(w http.ResponseWriter, r *http.Request, status int, code string, err error) {
	if i18n.FromRequest(r) == "" {
		// A writer may carry the user's fallback even though this older client declared no language.
		err = errors.New(err.Error())
		if code == "" {
			writeErr(w, status, err)
		} else {
			writeErrCode(w, status, code, err)
		}
		return
	}
	fallback := "extension-status.failed"
	if code == "plugin_failed" {
		fallback = "extension-status.view_invalid"
	} else if strings.Contains(r.URL.Path, "/-/auth") {
		fallback = "extension-status.auth_failed"
	}
	summary, detail := extensionErrorCopy(i18n.OfWriter(w).Or(i18n.FromRequest(r)), err, fallback)
	extra := map[string]any{}
	if detail != "" {
		extra["detail"] = detail
	}
	writeCode(w, status, code, summary, extra)
}

func writeExtensionJSON(w http.ResponseWriter, r *http.Request, value any) {
	l := i18n.FromRequest(r)
	if l == "" {
		writeJSON(w, value)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		writeJSON(w, value)
		return
	}
	writeJSON(w, json.RawMessage(i18n.ExtensionClientJSON(l, raw)))
}

func writeExtensionResponseErr(w http.ResponseWriter, r *http.Request, status int, err error) {
	writeExtensionResponseError(w, r, status, "", err)
}

func writeExtensionChangeError(w http.ResponseWriter, r *http.Request, status int, code string, err error) {
	if errors.Is(err, errNeedsTriageModel) {
		status, code = http.StatusConflict, "needs_triage_model"
	}
	writeExtensionResponseError(w, r, status, code, err)
}
