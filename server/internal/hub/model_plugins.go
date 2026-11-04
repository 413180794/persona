package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type ModelChoice struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type pluginModels struct {
	Models  []ModelChoice `json:"models"`
	Default string        `json:"default"`
}

func (h *Hub) pluginModelChoices(ctx context.Context, id string) (pluginModels, error) {
	var result pluginModels
	if h.plugins == nil {
		return result, errors.New("未接入插件")
	}
	p := h.plugins.get(strings.TrimPrefix(id, "plugin."))
	if p == nil || p.kind != "voice" || p.client == nil {
		return result, errors.New("语音插件不存在")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://plugin/models", nil)
	if err != nil {
		return result, err
	}
	response, err := p.client.Do(req)
	if err != nil {
		return result, errors.New("读不到插件模型列表")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, errors.New("插件模型列表暂不可用")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return result, errors.New("插件模型列表格式不正确")
	}
	return result, nil
}
