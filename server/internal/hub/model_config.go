package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/ai"
)

// ModelBinding is a sparse, non-secret selection; missing fields inherit a declared baseline.
type ModelBinding struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

type modelRecord struct {
	Revision int64        `json:"revision"`
	Binding  ModelBinding `json:"binding"`
}

type ModelProvider struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	API      string        `json:"api,omitempty"`
	Model    string        `json:"model,omitempty"`
	Machines []string      `json:"machines,omitempty"`
	Hidden   bool          `json:"hidden,omitempty"`
	HasKey   bool          `json:"has_key,omitempty"`
	Models   []ModelChoice `json:"models,omitempty"`
	Status   string        `json:"status,omitempty"`
}

type ModelUse struct {
	Name string `json:"name"`
}

type ModelPurpose struct {
	ID            string       `json:"id"`
	CategoryID    string       `json:"category_id,omitempty"`
	Uses          []ModelUse   `json:"uses,omitempty"`
	Name          string       `json:"name"`
	Group         string       `json:"group"`
	Kind          string       `json:"kind"`
	Parent        string       `json:"parent,omitempty"`
	MachineID     string       `json:"machine_id,omitempty"`
	Override      ModelBinding `json:"override"`
	Effective     ModelBinding `json:"effective"`
	Revision      int64        `json:"revision"`
	InheritedFrom string       `json:"inherited_from,omitempty"`
	Status        string       `json:"status,omitempty"`
	Limit         *ai.Pause    `json:"limit,omitempty"` // the connection is held: quota used up or busy (ai/limit.go)
	Fields        []string     `json:"fields"`
	ProviderIDs   []string     `json:"provider_ids"`
}

var modelCategoryIDs = []string{"assistant", "agent.new", "kb", "inbox.triage"}

var modelPurposes = []ModelPurpose{
	{ID: "assistant", Name: "主对话", Group: "默认模型", Kind: "launcher", Uses: []ModelUse{{Name: "与当前 persona 的直接对话"}, {Name: "定时任务"}}},
	{ID: "agent.new", Name: "Agents", Group: "默认模型", Kind: "launcher", Uses: []ModelUse{{Name: "新建 Agent"}, {Name: "AI 分身执行"}, {Name: "飞书 /agent"}}},
	{ID: "kb", Name: "信息处理", Group: "默认模型", Kind: "api", Uses: []ModelUse{{Name: "文本与邮件翻译"}, {Name: "草拟回复"}, {Name: "知识整理与会话压缩"}, {Name: "项目整理与事项提取"}, {Name: "对话标题与 Agent 标签名"}}},
	{ID: "inbox.triage", Name: "信息筛选", Group: "默认模型", Kind: "api", Uses: []ModelUse{{Name: "收件箱分拣与摘要"}, {Name: "重要性判断"}, {Name: "判断是否需要回复或交给 Agent"}}},
	{ID: "inbox.agent", Name: "AI 分身执行", Group: "Agents", Kind: "launcher", Parent: "agent.new"},
	{ID: "lark.agent", Name: "飞书 /agent", Group: "Agents", Kind: "launcher", Parent: "agent.new"},
	{ID: "inbox.judge", Name: "AI 分身判断器", Group: "信息筛选", Kind: "api", Parent: "inbox.triage"},
	{ID: "inbox.draft", Name: "草拟回复", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "translate", Name: "文本与邮件翻译", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "kb.compress", Name: "会话压缩与打标", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "kb.project", Name: "项目档案重写", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "kb.dedupe", Name: "项目去重", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "kb.items", Name: "事项提取与打标", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "kb.moments", Name: "公司圈关联", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "assistant.title", Name: "对话标题", Group: "信息处理", Kind: "api", Parent: "kb"},
	{ID: "agent.title", Name: "Agent 标签名", Group: "信息处理", Kind: "api", Parent: "kb"},
	// Scheduled tasks (早报…) run in the assistant's directory with its instructions, memory and tools: they are the
	// assistant's conversations, on 主对话's model, not new Agents (#1544; #697 had them under Agents, plain Claude Code
	// by default, which is not logged in on every hub machine).
	{ID: "schedule", Name: "定时任务", Group: "主对话", Kind: "launcher", Parent: "assistant"},
}

func modelCategoryID(id string) string {
	if strings.HasPrefix(id, "task.") {
		return "assistant"
	}
	for _, p := range modelPurposes {
		if p.ID == id && p.Parent != "" {
			return p.Parent
		}
	}
	return id
}

var errModelPurposeNotFound = errors.New("没有这个模型用途")

func modelKey(id string) string { return "model_config." + id }

func (h *Hub) modelRecord(ctx context.Context, id string) (modelRecord, error) {
	var r modelRecord
	if h.store != nil {
		_, err := h.store.Setting(ctx, modelKey(id), &r)
		return r, err
	}
	return r, nil
}

func (h *Hub) modelPurposeDef(ctx context.Context, id string) (ModelPurpose, error) {
	for _, p := range modelPurposes {
		if p.ID == id {
			p.CategoryID = modelCategoryID(id)
			if p.Kind == "launcher" && p.ID != "agent.new" {
				p.MachineID = h.assistantConfigLegacy(ctx).MachineID
			}
			if h.cfg != nil {
				if p.ID == "inbox.agent" && h.localMachine(h.cfg.Inbox.Agent.Machine) != nil {
					p.MachineID = h.cfg.Inbox.Agent.Machine
				}
			}
			return p, nil
		}
	}
	if strings.HasPrefix(id, "task.") {
		s := h.loadSchedule(ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.loaded {
			return ModelPurpose{}, errors.New("读不到定时任务")
		}
		for _, t := range s.tasks {
			if id == "task."+t.ID {
				return ModelPurpose{ID: id, CategoryID: "assistant", Name: t.label(), Group: "主对话", Kind: "launcher", Parent: "assistant", MachineID: t.MachineID}, nil
			}
		}
	}
	if strings.HasPrefix(id, "plugin.") && h.plugins != nil {
		for _, p := range h.plugins.list() {
			if id == "plugin."+p.ID && p.Kind == "voice" {
				return ModelPurpose{ID: id, Name: p.Name, Group: "插件", Kind: "plugin"}, nil
			}
		}
	}
	return ModelPurpose{}, errModelPurposeNotFound
}

// Legacy connections remain server-side and retain their existing field-wise inheritance.
func (h *Hub) legacyModelConfig(id string) ai.Config {
	var c ai.Config
	if h.cfg != nil {
		c = h.cfg.AI
	}
	if h.ai != nil && c.BaseURL == "" {
		c = h.ai.Config()
	}
	switch id {
	case "legacy.triage":
		if h.cfg != nil && h.cfg.Inbox.TriageAI != nil {
			c = h.cfg.Inbox.TriageAI.Over(c)
		} else if h.triageAI != nil {
			c = h.triageAI.Config()
		}
	case "legacy.judge":
		if h.cfg != nil && h.cfg.Inbox.JudgeAI != nil {
			c = h.cfg.Inbox.JudgeAI.Over(c)
		} else if h.judgeAI != nil {
			c = h.judgeAI.Config()
		}
	case "legacy.kb":
		if h.cfg != nil && h.cfg.KB.AI != nil {
			c = h.cfg.KB.AI.Over(c)
		} else {
			c = h.legacyModelConfig("legacy.triage")
		}
	}
	return c
}

func (h *Hub) modelProviders(ctx context.Context) []ModelProvider {
	out := []ModelProvider{}
	for _, x := range []struct{ id, name string }{{"legacy.ai", "hub 默认 API"}, {"legacy.triage", "hub 分拣 API"}, {"legacy.judge", "hub 判断 API"}, {"legacy.kb", "hub 知识 API"}} {
		c := h.legacyModelConfig(x.id)
		if c.BaseURL == "" {
			continue
		}
		apiName := c.API
		if apiName == "" {
			apiName = "messages"
		}
		out = append(out, ModelProvider{ID: x.id, Name: x.name, Kind: "api", API: apiName, Model: c.Model, HasKey: c.APIKeyFile != "" || c.APIKey != ""})
	}
	for _, v := range h.launcherViews(ctx) {
		kind := v.Kind
		if kind == "" {
			kind = "launcher"
		}
		apiName := v.API
		if kind == "api" && apiName == "" {
			apiName = "messages"
		}
		out = append(out, ModelProvider{ID: "launcher." + v.Name, Name: v.Name, Kind: kind, API: apiName, Model: v.Model, Machines: v.Machines, Hidden: v.Hidden, HasKey: v.HasKey})
	}
	if !slices.ContainsFunc(out, func(p ModelProvider) bool { return p.ID == "launcher.claude" }) {
		out = append(out, ModelProvider{ID: "launcher.claude", Name: "Claude Code 默认", Kind: "launcher"})
	}
	if h.plugins != nil {
		for _, p := range h.plugins.list() {
			if p.Kind == "voice" {
				models, err := h.pluginModelChoices(ctx, "plugin."+p.ID)
				provider := ModelProvider{ID: "plugin." + p.ID, Name: p.Name, Kind: "plugin", Model: models.Default, Models: models.Models}
				if err != nil {
					provider.Status = err.Error()
				}
				out = append(out, provider)
			}
		}
	}
	return out
}

func (h *Hub) modelBaseline(ctx context.Context, p ModelPurpose) (ModelBinding, string, error) {
	if p.Parent != "" {
		b, err := h.resolveModel(ctx, p.Parent)
		return b, p.Parent, err
	}
	if p.Kind == "api" {
		provider := "legacy.ai"
		if p.ID == "inbox.triage" {
			provider = "legacy.triage"
		}
		if p.ID == "kb" {
			provider = "legacy.kb"
		}
		c := h.legacyModelConfig(provider)
		if c.BaseURL == "" {
			// No hub.json connection for this use: fall back to a provider the hub can call itself, instead of failing
			// while a keyed Claude connection sits unused (#388). Not stored, so a later explicit choice still wins.
			if name := h.fallbackAPIProvider(ctx); name != "" {
				return ModelBinding{Provider: "launcher." + name}, modelAutoSource, nil
			}
		}
		return ModelBinding{Provider: provider, Model: c.Model, Effort: c.ReasoningEffort}, "hub 配置", nil
	}
	if p.ID == "assistant" {
		cfg := h.assistantConfigLegacy(ctx)
		return ModelBinding{Provider: "launcher." + cfg.Launcher}, "助手默认", nil
	}
	if p.Kind == "launcher" {
		return ModelBinding{Provider: "launcher.claude"}, "默认", nil
	}
	return ModelBinding{Provider: p.ID}, "插件默认", nil
}

// modelAutoSource is what 继承来源 says for an API use that runs on fallbackAPIProvider's pick.
const modelAutoSource = "自动选择（hub 上能直接调用的接入）"

// modelNoAPIStatus explains an API use with nothing to call: launchers like 「Claude Code 默认」 only start a CLI on a
// computer, so they never show up for these uses.
const modelNoAPIStatus = "没有能直接调用的模型：「Claude Code 默认」这类启动器只能在电脑上启动，不能用于此项。" +
	"在 设置 › 模型供应商 添加带 Key 的接入后选它"

// fallbackAPIProvider names the managed connection an unconfigured API use runs on: the first direct API provider,
// else the first keyed launcher speaking Messages (Responses launchers cannot serve these uses). Hidden ones and
// ones without an address, a default model or a key are skipped. "" when there is none.
func (h *Hub) fallbackAPIProvider(ctx context.Context) string {
	stored := h.storedLaunchers(ctx)
	hidden := h.hiddenLaunchers(ctx)
	usable := func(s storedLauncher) bool {
		return !slices.Contains(hidden, s.Name) && s.BaseURL != "" && s.Model != "" && s.KeySealed != ""
	}
	for _, s := range stored {
		if s.Kind == "api" && usable(s) {
			return s.Name
		}
	}
	for _, s := range stored {
		if s.Kind != "api" && s.API != "responses" && usable(s) {
			return s.Name
		}
	}
	return ""
}

func mergeModel(base, over ModelBinding) ModelBinding {
	if over.Provider != "" && over.Provider != base.Provider {
		base = ModelBinding{Provider: over.Provider}
	}
	if over.Provider != "" {
		base.Provider = over.Provider
	}
	if over.Model != "" {
		base.Model = over.Model
	}
	if over.Effort != "" {
		base.Effort = over.Effort
	}
	return base
}

func (h *Hub) resolveModel(ctx context.Context, id string) (ModelBinding, error) {
	p, err := h.modelPurposeDef(ctx, id)
	if err != nil {
		return ModelBinding{}, err
	}
	if p.CategoryID != "" && p.CategoryID != id {
		return h.resolveModel(ctx, p.CategoryID)
	}
	base, _, err := h.modelBaseline(ctx, p)
	if err != nil {
		return base, err
	}
	r, err := h.modelRecord(ctx, id)
	if err != nil {
		return base, fmt.Errorf("读取模型配置失败: %w", err)
	}
	return mergeModel(base, r.Binding), nil
}

func launcherProvider(name string) string {
	if name == "" {
		return ""
	}
	return "launcher." + name
}

func (h *Hub) apiConfig(ctx context.Context, b ModelBinding) (ai.Config, error) {
	var c ai.Config
	if strings.HasPrefix(b.Provider, "legacy.") {
		c = h.legacyModelConfig(b.Provider)
	} else if name, ok := strings.CutPrefix(b.Provider, "launcher."); ok {
		found := false
		for _, s := range h.storedLaunchers(ctx) {
			if s.Name == name {
				if s.Kind != "api" && s.API == "responses" {
					return c, errors.New("启动器不能作为 Responses API")
				}
				c = ai.Config{API: s.API, BaseURL: s.BaseURL, Model: s.Model}
				key, err := h.openKey(s.KeySealed)
				if err != nil {
					return c, errors.New("供应商密钥解密失败")
				}
				c.APIKey = key
				found = true
				break
			}
		}
		if !found {
			return c, errors.New("这个供应商不能在 hub 上直接调用 API")
		}
	} else {
		return c, errors.New("不是 API 供应商")
	}
	if b.Model != "" {
		c.Model = b.Model
	}
	c.ReasoningEffort = b.Effort
	if c.API != "" && c.API != "messages" && c.API != "responses" {
		return c, errors.New("不支持这个 API 协议")
	}
	if c.API != "responses" && b.Effort != "" {
		c.ReasoningEffort = ""
	} // Older Messages configurations carried an unused Responses-only field.
	return c, nil
}

func (h *Hub) purposeModel(id string) *ai.Client {
	return ai.NewResolved(func(ctx context.Context) (*ai.Client, error) {
		b, err := h.resolveModel(ctx, id)
		if err != nil {
			return nil, err
		}
		c, err := h.apiConfig(ctx, b)
		if err != nil {
			return nil, err
		}
		return ai.New(c), nil
	})
}

func (h *Hub) modelFingerprint(ctx context.Context, id string) (string, error) {
	b, err := h.resolveModel(ctx, id)
	if err != nil {
		return "", err
	}
	return h.modelBindingFingerprint(ctx, b)
}

func (h *Hub) modelPurpose(ctx context.Context, id string, providers []ModelProvider) (ModelPurpose, error) {
	p, err := h.modelPurposeDef(ctx, id)
	if err != nil {
		return p, err
	}
	recordID := id
	if p.CategoryID != "" {
		recordID = p.CategoryID
	}
	r, err := h.modelRecord(ctx, recordID)
	if err != nil {
		return p, err
	}
	p.Override, p.Revision = r.Binding, r.Revision
	readOnly := recordID != id
	if readOnly {
		p.Override = ModelBinding{}
	}
	p.Effective, err = h.resolveModel(ctx, id)
	if err != nil {
		return p, err
	}
	_, p.InheritedFrom, err = h.modelBaseline(ctx, p)
	if err != nil {
		return p, err
	}
	p.Fields = []string{"provider", "model"}
	p.ProviderIDs = []string{}
	for _, v := range providers {
		compatible := v.Kind == p.Kind
		if p.Kind == "api" && v.Kind == "launcher" {
			for _, s := range h.storedLaunchers(ctx) {
				if v.ID == "launcher."+s.Name {
					compatible = s.API != "responses"
					break
				}
			}
		}
		if p.Kind == "plugin" {
			compatible = v.ID == p.ID
		}
		if compatible && p.Kind == "launcher" && len(v.Machines) > 0 && p.MachineID != "" {
			compatible = slices.Contains(v.Machines, p.MachineID)
		}
		if compatible {
			p.ProviderIDs = append(p.ProviderIDs, v.ID)
		}
		if v.ID == p.Effective.Provider {
			if p.Effective.Model == "" {
				p.Effective.Model = v.Model
			}
			if p.Kind == "api" && v.API == "responses" {
				p.Fields = append(p.Fields, "effort")
			}
		}
	}
	if p.Kind == "launcher" {
		p.Fields = append(p.Fields, "effort")
	}
	p.Status = "未验证"
	if !slices.Contains(p.ProviderIDs, p.Effective.Provider) {
		p.Status = "供应商不存在或不兼容"
		if p.Kind == "api" && strings.HasPrefix(p.Effective.Provider, "legacy.") && h.legacyModelConfig(p.Effective.Provider).BaseURL == "" {
			p.Status = modelNoAPIStatus
		}
	} else if p.Kind == "api" && p.Effective.Model == "" {
		p.Status = "未配置模型"
	} else if p.Kind == "api" {
		if config, e := h.apiConfig(ctx, p.Effective); e != nil {
			p.Status = e.Error()
		} else if config.APIKey == "" && config.APIKeyFile == "" {
			p.Status = "未配置凭据"
		} else if pause := ai.Held(config); pause != nil {
			p.Status, p.Limit = pause.Text(), pause
		}
	} else if p.Kind == "plugin" {
		for _, provider := range providers {
			if provider.ID == p.Effective.Provider && provider.Status != "" {
				p.Status = provider.Status
				break
			}
		}
	} else if p.Kind == "launcher" && p.MachineID != "" {
		if m := h.localMachine(p.MachineID); m != nil {
			m.mu.Lock()
			online, info := m.online, m.info
			m.mu.Unlock()
			if !online {
				p.Status = "运行机器离线"
			} else if info.Version != "" && !info.ModelConfiguration {
				p.Status = "需要更新 node"
			}
		}
	}
	if readOnly {
		p.Fields = []string{}
		for _, category := range modelPurposes {
			if category.ID == recordID {
				p.InheritedFrom = category.Name
				break
			}
		}
	}
	return p, nil
}

type modelPatch struct {
	Revision *int64          `json:"revision"`
	Provider json.RawMessage `json:"provider"`
	Model    json.RawMessage `json:"model"`
	Effort   json.RawMessage `json:"effort"`
}

func patchModel(b ModelBinding, in modelPatch) (ModelBinding, error) {
	if in.Provider != nil {
		var provider string
		if string(in.Provider) != "null" {
			if err := json.Unmarshal(in.Provider, &provider); err != nil {
				return b, errors.New("供应商要是字符串或 null")
			}
		}
		if provider != b.Provider {
			b.Model, b.Effort = "", ""
		}
	}
	for _, f := range []struct {
		raw json.RawMessage
		dst *string
	}{{in.Provider, &b.Provider}, {in.Model, &b.Model}, {in.Effort, &b.Effort}} {
		if f.raw == nil {
			continue
		}
		if string(f.raw) == "null" {
			*f.dst = ""
			continue
		}
		var v string
		if err := json.Unmarshal(f.raw, &v); err != nil {
			return b, errors.New("模型字段要是字符串或 null")
		}
		if v == "" || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\r\n\x00") || len(v) > 512 {
			return b, errors.New("模型字段不能为空或包含换行")
		}
		*f.dst = v
	}
	return b, nil
}

func (h *Hub) saveModelRecord(ctx context.Context, id string, revision int64, b ModelBinding) error {
	if h.store == nil {
		return errors.New("没有数据库，不能保存模型配置")
	}
	r := modelRecord{Revision: revision + 1, Binding: b}
	if s, ok := h.store.(interface {
		CompareSetting(context.Context, string, int64, any) (bool, error)
	}); ok {
		updated, err := s.CompareSetting(ctx, modelKey(id), revision, r)
		if err != nil {
			return err
		}
		if !updated {
			return errModelConflict
		}
		return nil
	}
	// Test stores share this lock with legacy writes; production always uses database CAS.
	old, err := h.modelRecord(ctx, id)
	if err != nil {
		return err
	}
	if old.Revision != revision {
		return errModelConflict
	}
	return h.store.PutSetting(ctx, modelKey(id), r)
}

var errModelConflict = errors.New("配置已被修改，请重新加载后保存")

func (h *Hub) validateModel(ctx context.Context, p ModelPurpose, b ModelBinding, providers []ModelProvider) error {
	var provider *ModelProvider
	for i := range providers {
		if providers[i].ID == b.Provider {
			provider = &providers[i]
			break
		}
	}
	if provider == nil {
		return errors.New("供应商不存在")
	}
	if !slices.Contains(p.ProviderIDs, b.Provider) {
		return errors.New("供应商不支持这个用途")
	}
	if b.Effort != "" && !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, b.Effort) {
		return errors.New("思考等级不支持")
	}
	if p.Kind == "api" {
		_, err := h.apiConfig(ctx, b)
		return err
	}
	if p.Kind == "plugin" {
		if b.Effort != "" {
			return errors.New("插件不支持思考等级")
		}
		if b.Model != "" {
			models, err := h.pluginModelChoices(ctx, p.ID)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(models.Models, func(m ModelChoice) bool { return m.ID == b.Model }) {
				return errors.New("插件没有这个模型")
			}
		}
		return nil
	}
	if p.Kind == "launcher" {
		if len(provider.Machines) > 0 && p.MachineID != "" && !slices.Contains(provider.Machines, p.MachineID) {
			return errors.New("运行这个用途的电脑没有该启动器")
		}
		if b.Model != "" {
			m := h.localMachine(p.MachineID)
			if m == nil && len(provider.Machines) > 0 {
				m = h.localMachine(provider.Machines[0])
			}
			if m != nil {
				var result struct {
					Agent string `json:"agent"`
				}
				if err := m.call(ctx, "launchers.model.check", map[string]any{"name": strings.TrimPrefix(b.Provider, "launcher."), "model": b.Model}, &result); err != nil {
					return fmt.Errorf("无法验证启动器的具体模型: %w", err)
				}
				if p.ID == "agent.new" && result.Agent != "claude" && result.Agent != "codex" {
					return errors.New("普通 Agent 的启动只支持 Claude Code 或 Codex 启动器")
				}
				if p.ID == "agent.new" && result.Agent == "codex" && b.Effort != "" {
					return errors.New("新建 Codex 的思考强度由其启动器管理，请恢复继承")
				}
			}
		}
	}
	return nil
}

func (h *Hub) modelConfigRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/model-config", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		providers := h.modelProviders(ctx)
		purposes := []ModelPurpose{}
		for _, id := range modelCategoryIDs {
			p, err := h.modelPurpose(ctx, id, providers)
			if err != nil {
				writeErr(w, 500, err)
				return
			}
			purposes = append(purposes, p)
		}
		writeJSON(w, map[string]any{"purposes": purposes, "providers": providers})
	})
	detail := func(w http.ResponseWriter, r *http.Request) {
		providers := h.modelProviders(r.Context())
		p, err := h.modelPurpose(r.Context(), r.PathValue("id"), providers)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errModelPurposeNotFound) {
				status = http.StatusNotFound
			}
			writeErr(w, status, err)
			return
		}
		writeJSON(w, map[string]any{"purpose": p, "providers": providers})
	}
	api.HandleFunc("GET /api/v1/model-config/purposes/{id}", detail)
	api.HandleFunc("GET /api/v1/model-config/providers/{id}/models", h.providerModels)
	api.HandleFunc("PATCH /api/v1/model-config/purposes/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in modelPatch
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeErr(w, 400, err)
			return
		}
		if in.Revision == nil || *in.Revision < 0 {
			writeErr(w, 400, errors.New("需要配置版本"))
			return
		}
		h.modelMu.Lock()
		defer h.modelMu.Unlock()
		ctx, id := r.Context(), r.PathValue("id")
		providers := h.modelProviders(ctx)
		p, err := h.modelPurpose(ctx, id, providers)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errModelPurposeNotFound) {
				status = http.StatusNotFound
			}
			writeErr(w, status, err)
			return
		}
		if p.CategoryID != "" && p.CategoryID != id {
			writeErr(w, http.StatusBadRequest, errors.New("用途模型只读，请修改所属分类的模型配置"))
			return
		}
		if p.Revision != *in.Revision {
			writeErr(w, 409, errModelConflict)
			return
		}
		b, err := patchModel(p.Override, in)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		base, _, err := h.modelBaseline(ctx, p)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		effective := mergeModel(base, b)
		if in.Effort != nil && string(in.Effort) != "null" && p.Kind == "api" {
			config, e := h.apiConfig(ctx, effective)
			if e != nil {
				writeErr(w, 400, e)
				return
			}
			if config.API != "responses" {
				writeErr(w, 400, errors.New("这个 API 接入不支持思考等级"))
				return
			}
		}
		if b != (ModelBinding{}) {
			if err = h.validateModel(ctx, p, effective, providers); err != nil {
				writeErr(w, 400, err)
				return
			}
		}
		err = h.saveModelRecord(ctx, id, p.Revision, b)
		if err != nil {
			status := 500
			if errors.Is(err, errModelConflict) {
				status = 409
			}
			writeErr(w, status, err)
			return
		}
		detail(w, r)
	})
}

func (h *Hub) modelBindingFingerprint(ctx context.Context, b ModelBinding) (string, error) {
	raw, _ := json.Marshal(b)
	if strings.HasPrefix(b.Provider, "legacy.") {
		c := h.legacyModelConfig(b.Provider)
		v, _ := json.Marshal(c)
		raw = append(raw, v...)
	}
	if name, ok := strings.CutPrefix(b.Provider, "launcher."); ok {
		for _, s := range h.storedLaunchers(ctx) {
			if s.Name == name {
				v, _ := json.Marshal(s)
				raw = append(raw, v...)
				break
			}
		}
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// applyModel selects the execution profile without mutating permission mode.
func (h *Hub) applyModel(ctx context.Context, id string, params map[string]any) error {
	b, err := h.resolveModel(ctx, id)
	if err != nil {
		return err
	}
	if name, _ := params["launcher"].(string); name != "" && launcherProvider(name) != b.Provider {
		b = ModelBinding{Provider: launcherProvider(name)}
	}
	if model, _ := params["model"].(string); model != "" {
		b.Model = model
	}
	if effort, _ := params["effort"].(string); effort != "" {
		b.Effort = effort
	}
	return h.finishModel(ctx, id, params, b)
}

// applyPickedModel is applyModel for a launcher/model picked for one send (the chat's menu): with no model the
// launcher's own default stays, instead of the purpose's configured one.
func (h *Hub) applyPickedModel(ctx context.Context, params map[string]any) error {
	name, _ := params["launcher"].(string)
	if name == "" {
		return nil
	}
	b := ModelBinding{Provider: launcherProvider(name)}
	if m, _ := params["model"].(string); m != "" {
		b.Model = m
	}
	if e, _ := params["effort"].(string); e != "" {
		b.Effort = e
	}
	return h.finishModel(ctx, "assistant", params, b)
}

// finishModel puts a final binding into params: the launcher name, the model / effort set, the execution fingerprint.
func (h *Hub) finishModel(ctx context.Context, id string, params map[string]any, b ModelBinding) error {
	name, ok := strings.CutPrefix(b.Provider, "launcher.")
	if !ok {
		if strings.HasPrefix(id, "plugin.") {
			params["model"] = b.Model
			return nil
		}
		return errors.New("这个用途不是启动器")
	}
	for _, provider := range h.storedLaunchers(ctx) {
		if provider.Name == name && provider.Kind == "api" {
			return errors.New("这个用途引用了直接 API 接入，不能作为启动器运行")
		}
	}
	params["launcher"] = name
	if b.Model != "" {
		params["model"] = b.Model
	}
	if b.Effort != "" {
		params["effort"] = b.Effort
	}
	fingerprint, err := h.modelBindingFingerprint(ctx, b)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(append([]byte(fingerprint), raw...))
	params["model_fingerprint"] = hex.EncodeToString(sum[:])
	return nil
}
