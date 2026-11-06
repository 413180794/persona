package extension

import (
	"errors"

	"github.com/nwverse/persona/server/internal/i18n"
)

// StatusErrorText translates hub-owned status errors, retaining raw parser, OS and extension diagnostics.
func StatusErrorText(l i18n.Lang, err error) string {
	if err == nil {
		return ""
	}
	var field *FieldError
	if errors.As(err, &field) && field.Error() == err.Error() {
		return field.Text(l)
	}
	return i18n.ExtensionStatusText(l, i18n.Localize(err, l))
}

// Text preserves the path supplied to old clients and renders the validation message for this request.
func (e *FieldError) Text(l i18n.Lang) string { return e.Path + ": " + e.MessageText(l) }

// MessageText is the localized field explanation, without its machine-readable path.
func (e *FieldError) MessageText(l i18n.Lang) string {
	if e.cause != nil {
		return StatusErrorText(l, e.cause)
	}
	return i18n.ExtensionStatusText(l, e.Message)
}

func fieldError(path string, err error) *FieldError {
	return &FieldError{Path: path, Message: err.Error(), cause: err}
}

// WarningTexts translates a copy of canonical registry warnings, never the manifest's authored labels.
func (p *Extension) WarningTexts(l i18n.Lang) []string {
	out := make([]string, len(p.Warnings))
	for i, text := range p.Warnings {
		out[i] = i18n.ExtensionStatusText(l, text)
	}
	return out
}
