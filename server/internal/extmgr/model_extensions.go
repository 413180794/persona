package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
)

// ModelChoice is one model a voice plugin offers (plugin.<id> in the assistant's model configuration).
type ModelChoice = assistantwire.ModelChoice

type extensionModels struct {
	Models  []ModelChoice `json:"models"`
	Default string        `json:"default"`
}

func (h *Manager) extensionModelChoices(ctx context.Context, id string) (extensionModels, error) {
	var result extensionModels
	if h.extensions == nil {
		return result, i18n.Err("hub-model_extensions.no_extensions")
	}
	p := h.extensions.get(strings.TrimPrefix(id, "plugin."))
	if p == nil || p.kind != "voice" || p.client == nil {
		return result, i18n.Err("hub-model_extensions.no_voice")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://plugin/models", nil)
	if err != nil {
		return result, err
	}
	response, err := p.client.Do(req)
	if err != nil {
		return result, i18n.Err("hub-model_extensions.unreadable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, i18n.Err("hub-model_extensions.unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return result, i18n.Err("hub-model_extensions.malformed")
	}
	return result, nil
}
