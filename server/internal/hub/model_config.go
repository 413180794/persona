package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// ModelBinding is a sparse, non-secret selection; missing fields inherit a declared baseline.
type ModelBinding struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	// Runtime is how 主对话 runs Claude (proto.AssistantRuntimeCLI or AssistantRuntimeAgentSDK; "" is the CLI). Only the
	// assistant purpose takes it; it is the purpose's, so it stays when the provider changes.
	Runtime string `json:"runtime,omitempty"`
}

// assistantRuntimes is what GET /model-config offers for 主对话's runtime.
var assistantRuntimes = []string{proto.AssistantRuntimeCLI, proto.AssistantRuntimeAgentSDK}

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
	Runtimes      []string     `json:"runtimes,omitempty"` // assistant only: the runtimes it can run on
}

var modelCategoryIDs = []string{"assistant", "agent.new", "kb", "inbox.triage"}

// modelPurposes are the uses; Name, Group and Uses hold catalog names (hub-model_config), read in a language by
// localized.
var modelPurposes = []ModelPurpose{
	{ID: "assistant", Group: "group_default", Kind: "launcher", Runtimes: assistantRuntimes, Uses: []ModelUse{{Name: "use_chat"}, {Name: "use_schedule"}}},
	{ID: "agent.new", Group: "group_default", Kind: "launcher", Uses: []ModelUse{{Name: "use_new_agent"}, {Name: "use_inbox_agent"}, {Name: "use_lark_agent"}}},
	{ID: "kb", Group: "group_default", Kind: "api", Uses: []ModelUse{{Name: "use_translate"}, {Name: "use_draft"}, {Name: "use_knowledge"}, {Name: "use_projects"}, {Name: "use_titles"}}},
	{ID: "inbox.triage", Group: "group_default", Kind: "api", Uses: []ModelUse{{Name: "use_triage"}, {Name: "use_importance"}, {Name: "use_reply_or_agent"}}},
	{ID: "inbox.agent", Group: "group_agents", Kind: "launcher", Parent: "agent.new"},
	{ID: "lark.agent", Group: "group_agents", Kind: "launcher", Parent: "agent.new"},
	{ID: "inbox.judge", Group: "group_triage", Kind: "api", Parent: "inbox.triage"},
	{ID: "agent.triage", Group: "group_triage", Kind: "api", Parent: "inbox.triage"},
	{ID: "inbox.draft", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "translate", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "kb.compress", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "kb.project", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "kb.dedupe", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "kb.items", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "kb.moments", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "assistant.title", Group: "group_processing", Kind: "api", Parent: "kb"},
	{ID: "agent.title", Group: "group_processing", Kind: "api", Parent: "kb"},
	// Scheduled tasks (早报…) run in the assistant's directory with its instructions, memory and tools: they are the
	// assistant's conversations, on 主对话's model, not new Agents (#1544; #697 had them under Agents, plain Claude Code
	// by default, which is not logged in on every hub machine).
	{ID: "schedule", Group: "group_assistant", Kind: "launcher", Parent: "assistant"},
}

// mc is a hub-model_config text in l.
func mc(l i18n.Lang, name string, args ...i18n.Args) string {
	return i18n.T(l, "hub-model_config."+name, args...)
}

// purposeName is a built-in use's name in l.
func purposeName(l i18n.Lang, id string) string {
	return mc(l, "name_"+strings.ReplaceAll(id, ".", "_"))
}

// localized is a built-in use's definition with its texts in l.
func (p ModelPurpose) localized(l i18n.Lang) ModelPurpose {
	p.Name, p.Group = purposeName(l, p.ID), mc(l, p.Group)
	if len(p.Uses) > 0 {
		uses := make([]ModelUse, len(p.Uses))
		for i, u := range p.Uses {
			uses[i] = ModelUse{Name: mc(l, u.Name)}
		}
		p.Uses = uses
	}
	return p
}

// requestCtxLang is the language of what ctx is answering: the request's (http.go puts it in the context), else the
// user's.
func (h *Hub) requestCtxLang(ctx context.Context) i18n.Lang {
	return i18n.From(ctx).Or(h.userLang())
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

var errModelPurposeNotFound = i18n.Err("hub-model_config.err_purpose_not_found")

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
	l := h.requestCtxLang(ctx)
	for _, p := range modelPurposes {
		if p.ID == id {
			p = p.localized(l)
			p.CategoryID = modelCategoryID(id)
			if p.Kind == "launcher" && p.ID != "agent.new" {
				p.MachineID = h.assistantProfile().MachineID
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
		tasks, ok := h.assistantTasks() // the assistant's scheduled tasks, as it reported them
		if !ok {
			return ModelPurpose{}, i18n.Err("hub-model_config.err_read_tasks")
		}
		for _, t := range tasks {
			if id == "task."+t.ID {
				return ModelPurpose{ID: id, CategoryID: "assistant", Name: t.Label, Group: mc(l, "group_assistant"), Kind: "launcher", Parent: "assistant", MachineID: t.MachineID}, nil
			}
		}
	}
	if strings.HasPrefix(id, "plugin.") && h.extensions != nil {
		for _, p := range h.extensions.list() {
			if id == "plugin."+p.ID && p.Kind == "voice" {
				return ModelPurpose{ID: id, Name: p.Name, Group: mc(l, "group_extensions"), Kind: "plugin"}, nil
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
	l := h.requestCtxLang(ctx)
	out := []ModelProvider{}
	for _, x := range []struct{ id, name string }{{"legacy.ai", mc(l, "provider_legacy_ai")}, {"legacy.triage", mc(l, "provider_legacy_triage")}, {"legacy.judge", mc(l, "provider_legacy_judge")}, {"legacy.kb", mc(l, "provider_legacy_kb")}} {
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
		out = append(out, ModelProvider{ID: "launcher.claude", Name: mc(l, "provider_claude_default"), Kind: "launcher"})
	}
	if h.extensions != nil {
		for _, p := range h.extensions.list() {
			if p.Kind == "voice" {
				models, err := h.extensionModelChoices(ctx, "plugin."+p.ID)
				provider := ModelProvider{ID: "plugin." + p.ID, Name: p.Name, Kind: "plugin", Model: models.Default, Models: models.Models}
				if err != nil {
					provider.Status = i18n.Localize(err, l)
				}
				out = append(out, provider)
			}
		}
	}
	return out
}

func (h *Hub) modelBaseline(ctx context.Context, p ModelPurpose) (ModelBinding, string, error) {
	l := h.requestCtxLang(ctx)
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
				return ModelBinding{Provider: "launcher." + name}, mc(l, modelAutoSource), nil
			}
		}
		return ModelBinding{Provider: provider, Model: c.Model, Effort: c.ReasoningEffort}, mc(l, "source_hub_config"), nil
	}
	if p.ID == "assistant" { // the assistant's own launcher (its settings, else hub.json's)
		return ModelBinding{Provider: "launcher." + h.assistantProfile().Launcher}, mc(l, "source_assistant_default"), nil
	}
	if p.Kind == "launcher" {
		return ModelBinding{Provider: "launcher.claude"}, mc(l, "source_default"), nil
	}
	return ModelBinding{Provider: p.ID}, mc(l, "source_extension_default"), nil
}

// modelAutoSource is (the catalog name of) what 继承来源 says for an API use that runs on fallbackAPIProvider's pick.
const modelAutoSource = "source_auto"

// modelNoAPIStatus explains an API use with nothing to call: launchers like 「Claude Code 默认」 only start a CLI on a
// computer, so they never show up for these uses. A catalog name.
const modelNoAPIStatus = "status_no_api"

// modelUnverified is the status of a use nothing is known to be wrong with. Not translated: the apps compare it to
// this text (iOS ModelConfigurationView, Android HttpVoiceExtensions) to leave it out.
const modelUnverified = "未验证"

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
		base = ModelBinding{Provider: over.Provider, Runtime: base.Runtime}
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
	if over.Runtime != "" {
		base.Runtime = over.Runtime
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
		return base, i18n.Err("hub-model_config.err_read_config", i18n.Args{"error": err.Error()})
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
					return c, i18n.Err("hub-model_config.err_launcher_not_responses")
				}
				c = ai.Config{API: s.API, BaseURL: s.BaseURL, Model: s.Model, Bearer: s.BearerAuth()}
				key, err := h.openKey(s.KeySealed)
				if err != nil {
					return c, i18n.Err("hub-model_config.err_key_decrypt")
				}
				c.APIKey = key
				found = true
				break
			}
		}
		if !found {
			return c, i18n.Err("hub-model_config.err_not_callable")
		}
	} else {
		return c, i18n.Err("hub-model_config.err_not_api")
	}
	if b.Model != "" {
		c.Model = b.Model
	}
	c.ReasoningEffort = b.Effort
	if c.API != "" && c.API != "messages" && c.API != "responses" {
		return c, i18n.Err("hub-model_config.err_api_protocol")
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
		if p.Kind == "launcher" && v.Kind == "api" { // a direct Messages API provider starts Claude on its gateway (#2455)
			compatible = v.API != "responses"
		}
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
	if len(p.Runtimes) > 0 {
		p.Fields = append(p.Fields, "runtime")
	}
	l := h.requestCtxLang(ctx)
	p.Status = modelUnverified
	if !slices.Contains(p.ProviderIDs, p.Effective.Provider) {
		p.Status = mc(l, "status_provider_incompatible")
		if p.Kind == "api" && strings.HasPrefix(p.Effective.Provider, "legacy.") && h.legacyModelConfig(p.Effective.Provider).BaseURL == "" {
			p.Status = mc(l, modelNoAPIStatus)
		}
	} else if p.Kind == "api" && p.Effective.Model == "" {
		p.Status = mc(l, "status_no_model")
	} else if p.Kind == "api" {
		if config, e := h.apiConfig(ctx, p.Effective); e != nil {
			p.Status = i18n.Localize(e, l)
		} else if config.APIKey == "" && config.APIKeyFile == "" {
			p.Status = mc(l, "status_no_credentials")
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
				p.Status = mc(l, "status_machine_offline")
			} else if info.Version != "" && !info.ModelConfiguration {
				p.Status = mc(l, "status_node_outdated")
			}
		}
	}
	if readOnly {
		p.Fields = []string{}
		for _, category := range modelPurposes {
			if category.ID == recordID {
				p.InheritedFrom = purposeName(l, category.ID)
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
	Runtime  json.RawMessage `json:"runtime"`
}

func patchModel(b ModelBinding, in modelPatch) (ModelBinding, error) {
	if in.Provider != nil {
		var provider string
		if string(in.Provider) != "null" {
			if err := json.Unmarshal(in.Provider, &provider); err != nil {
				return b, i18n.Err("hub-model_config.err_provider_type")
			}
		}
		if provider != b.Provider {
			b.Model, b.Effort = "", ""
		}
	}
	for _, f := range []struct {
		raw json.RawMessage
		dst *string
	}{{in.Provider, &b.Provider}, {in.Model, &b.Model}, {in.Effort, &b.Effort}, {in.Runtime, &b.Runtime}} {
		if f.raw == nil {
			continue
		}
		if string(f.raw) == "null" {
			*f.dst = ""
			continue
		}
		var v string
		if err := json.Unmarshal(f.raw, &v); err != nil {
			return b, i18n.Err("hub-model_config.err_field_type")
		}
		if v == "" || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\r\n\x00") || len(v) > 512 {
			return b, i18n.Err("hub-model_config.err_field_invalid")
		}
		*f.dst = v
	}
	if b.Runtime != "" && !slices.Contains(assistantRuntimes, b.Runtime) {
		return b, i18n.Err("hub-model_config.err_runtime_invalid")
	}
	if b.Runtime == proto.AssistantRuntimeCLI {
		b.Runtime = "" // the default, stored as none: an explicit cli fingerprints like no choice
	}
	return b, nil
}

func (h *Hub) saveModelRecord(ctx context.Context, id string, revision int64, b ModelBinding) error {
	if h.store == nil {
		return i18n.Err("hub-model_config.err_no_database")
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

var errModelConflict = i18n.Err("hub-model_config.err_conflict")

func (h *Hub) validateModel(ctx context.Context, p ModelPurpose, b ModelBinding, providers []ModelProvider) error {
	var provider *ModelProvider
	for i := range providers {
		if providers[i].ID == b.Provider {
			provider = &providers[i]
			break
		}
	}
	if provider == nil {
		return i18n.Err("hub-model_config.err_provider_missing")
	}
	if !slices.Contains(p.ProviderIDs, b.Provider) {
		return i18n.Err("hub-model_config.err_provider_unsupported")
	}
	if b.Effort != "" && !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, b.Effort) {
		return i18n.Err("hub-model_config.err_effort_unsupported")
	}
	if p.Kind == "api" {
		_, err := h.apiConfig(ctx, b)
		return err
	}
	if p.Kind == "plugin" {
		if b.Effort != "" {
			return i18n.Err("hub-model_config.err_extension_effort")
		}
		if b.Model != "" {
			models, err := h.extensionModelChoices(ctx, p.ID)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(models.Models, func(m ModelChoice) bool { return m.ID == b.Model }) {
				return i18n.Err("hub-model_config.err_extension_model")
			}
		}
		return nil
	}
	if p.Kind == "launcher" {
		if len(provider.Machines) > 0 && p.MachineID != "" && !slices.Contains(provider.Machines, p.MachineID) {
			return i18n.Err("hub-model_config.err_launcher_not_on_machine")
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
				name := strings.TrimPrefix(b.Provider, "launcher.")
				params := map[string]any{"name": name, "model": b.Model, "configured": m.configuredLauncher(name)}
				if err := m.call(ctx, "launchers.model.check", params, &result); err != nil {
					return i18n.Err("hub-model_config.err_check_model", i18n.Args{"error": i18n.Localize(err, h.requestCtxLang(ctx))})
				}
				if p.ID == "agent.new" && result.Agent != "claude" && result.Agent != "codex" {
					return i18n.Err("hub-model_config.err_agent_launcher")
				}
				if p.ID == "agent.new" && result.Agent == "codex" && b.Effort != "" {
					return i18n.Err("hub-model_config.err_codex_effort")
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
			writeErr(w, 400, i18n.Err("hub-model_config.err_revision_required"))
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
			writeErr(w, http.StatusBadRequest, i18n.Err("hub-model_config.err_purpose_readonly"))
			return
		}
		if p.Revision != *in.Revision {
			writeErr(w, 409, errModelConflict)
			return
		}
		if in.Runtime != nil && string(in.Runtime) != "null" && len(p.Runtimes) == 0 {
			writeErr(w, 400, i18n.Err("hub-model_config.err_runtime_assistant_only"))
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
		if body := h.triageInUse(ctx, id, effective); body != nil { // plugins that need it are on (extension_inbox.go)
			writeErrorBody(w, http.StatusUnprocessableEntity, body)
			return
		}
		if in.Effort != nil && string(in.Effort) != "null" && p.Kind == "api" {
			config, e := h.apiConfig(ctx, effective)
			if e != nil {
				writeErr(w, 400, e)
				return
			}
			if config.API != "responses" {
				writeErr(w, 400, i18n.Err("hub-model_config.err_api_effort"))
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
		b = ModelBinding{Provider: launcherProvider(name), Runtime: b.Runtime}
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
	// A pick is a launcher and model for one message; the runtime stays 主对话's.
	configured, err := h.resolveModel(ctx, "assistant")
	if err != nil {
		return err
	}
	b.Runtime = configured.Runtime
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
		return i18n.Err("hub-model_config.err_not_launcher")
	}
	for _, provider := range h.storedLaunchers(ctx) {
		// A direct API provider speaking Messages starts Claude on its gateway (node, contract #2455 §2); Responses
		// cannot.
		if provider.Name == name && provider.Kind == "api" && provider.API == "responses" {
			return i18n.Err("hub-model_config.err_api_as_launcher")
		}
	}
	if b.Runtime != "" {
		params["runtime"] = b.Runtime
	} else {
		delete(params, "runtime") // the CLI: nothing a caller left
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
