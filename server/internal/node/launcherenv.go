package node

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// A provider's environment for claude (#2455, docs/launchers.md): one pure function both ways of running the main
// conversation and every Agent use, the CLI (its --settings env, `jarvisd launch`'s env) and the Agent SDK bridge (the
// child's env and settings.env). Cindy's env-builder, the Anthropic part.

// legacyEnvClear: what `jarvisd launch` has always dropped from the shell (another launcher or account) before a
// managed launcher from before #2455 runs. A trailing "*" is a prefix.
var legacyEnvClear = []string{"ANTHROPIC_*", "CLAUDE_CODE_USE_BEDROCK*", "CLAUDE_CODE_USE_VERTEX*",
	"CLAUDE_CODE_USE_FOUNDRY*", "CLAUDE_CODE_OAUTH_TOKEN*", "CLAUDE_CODE_SUBAGENT_MODEL*"}

// providerEnvClear: what claude must not inherit before a provider's own env is set: legacyEnvClear and every other
// variable providerEnv may set but the proxy: the shell's stays (a gateway reachable only through it) unless the
// provider has its own (proxyEnvClear).
var providerEnvClear = append(slices.Clone(legacyEnvClear), "CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_CODE_MAX_CONTEXT_TOKENS",
	"CLAUDE_CODE_AUTO_COMPACT_WINDOW", "API_TIMEOUT_MS")

// proxyEnvClear: the shell's proxy, dropped when the provider sets its own.
var proxyEnvClear = []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}

// envClearFor is what to drop before l runs: a launcher from before #2455 keeps the shell's proxy and timeout, as it
// always has; a provider keeps the shell's proxy unless it has one.
func envClearFor(l proto.ManagedLauncher) []string {
	if providerShaped(l) {
		if l.Proxy != "" {
			return append(slices.Clone(providerEnvClear), proxyEnvClear...)
		}
		return providerEnvClear
	}
	return legacyEnvClear
}

// withoutEnv is environ (KEY=value entries) without the variables clear names.
func withoutEnv(environ, clear []string) []string {
	return slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		for _, c := range clear {
			if p, ok := strings.CutSuffix(c, "*"); ok && strings.HasPrefix(key, p) || key == c {
				return true
			}
		}
		return false
	})
}

// withoutProviderEnv is environ without what providerEnvClear names (the Agent SDK bridge's env).
func withoutProviderEnv(environ []string) []string { return withoutEnv(environ, providerEnvClear) }

const (
	// oneMillionSuffix: Claude Code runs a model with a 1M context window only when its name says so.
	oneMillionSuffix = "[1m]"
	// standardWindow: the window Claude Code assumes for a model, and the main conversation's compact window.
	standardWindow = 200000
	// gatewayTimeoutMS: API_TIMEOUT_MS for a gateway that sets none (slow third-party gateways, cindy's default).
	gatewayTimeoutMS = 3000000
)

// providerModel is what l knows of model m ("<id>" or "<id>[1m]").
func providerModel(l proto.ManagedLauncher, m string) (proto.ProviderModel, bool) {
	id := strings.TrimSuffix(m, oneMillionSuffix)
	for _, pm := range l.Models {
		if pm.ID == m || pm.ID == id {
			return pm, true
		}
	}
	return proto.ProviderModel{}, false
}

// sdkModel is the name Claude Code runs m (pm what is known of it) as: with [1m] for a window over 200K.
func sdkModel(m string, pm proto.ProviderModel) string {
	if pm.ContextWindow > standardWindow && !strings.HasSuffix(m, oneMillionSuffix) {
		return m + oneMillionSuffix
	}
	return m
}

// providerShaped: a provider as #2455 models it (an API provider, or any of its fields set), which gets a gateway's
// extras (no nonessential traffic, a long timeout). A launcher from before gets exactly the env it always had.
func providerShaped(l proto.ManagedLauncher) bool {
	return l.Kind == "api" || len(l.Models) > 0 || l.OpusModel != "" || l.SonnetModel != "" || l.HaikuModel != "" ||
		l.FableModel != "" || l.SubagentForce || l.Headers != "" || l.HasHeaders || l.Proxy != "" || l.NoProxy != "" ||
		l.TimeoutMS != 0 || l.CompactWindow != 0 || l.MaxContext != 0 || l.Effort != "" || l.Source != ""
}

// providerEnv is the environment claude runs provider l with, for this model and effort ("" for the provider's own)
// and the main conversation or not. A "" value means set empty: the variable must not come from anywhere else (the
// other credential of the two, which a gateway seeing both refuses with 401). What the shell carries is cleared
// first (providerEnvClear); settings env cannot unset, so a value here is the only way to override one there.
//
// Kept as it was for a launcher from before #2455 (managedEnvOrder): the gateway URL always (empty for none); no key
// stored (or the hub could not open it), neither credential, so Claude uses whatever sign-in it has; no model, none of
// the model variables. The Haiku tier is HaikuModel, else SmallModel (what it was), else the model.
func providerEnv(l proto.ManagedLauncher, model, effort string, mainConversation bool) map[string]string {
	env := map[string]string{"ANTHROPIC_BASE_URL": l.BaseURL}
	if l.BaseURL != "" && providerShaped(l) {
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		env["API_TIMEOUT_MS"] = strconv.Itoa(cmp.Or(l.TimeoutMS, gatewayTimeoutMS))
	} else if l.TimeoutMS > 0 {
		env["API_TIMEOUT_MS"] = strconv.Itoa(l.TimeoutMS)
	}
	switch {
	case l.APIKey == "":
	case l.BearerAuth():
		env["ANTHROPIC_AUTH_TOKEN"], env["ANTHROPIC_API_KEY"] = strings.TrimPrefix(l.APIKey, "Bearer "), ""
	default:
		env["ANTHROPIC_API_KEY"], env["ANTHROPIC_AUTH_TOKEN"] = l.APIKey, ""
	}
	set := func(k, v string) {
		if v != "" {
			env[k] = v
		}
	}
	m := cmp.Or(model, l.Model)
	pm, _ := providerModel(l, m)
	run := sdkModel(m, pm)
	set("ANTHROPIC_MODEL", run)
	set("ANTHROPIC_DEFAULT_OPUS_MODEL", cmp.Or(l.OpusModel, run))
	set("ANTHROPIC_DEFAULT_SONNET_MODEL", cmp.Or(l.SonnetModel, run))
	set("ANTHROPIC_DEFAULT_FABLE_MODEL", cmp.Or(l.FableModel, run))
	set("ANTHROPIC_DEFAULT_HAIKU_MODEL", cmp.Or(l.HaikuModel, l.SmallModel, run))
	set("ANTHROPIC_SMALL_FAST_MODEL", cmp.Or(l.SmallModel, run))
	// Subagents follow the model unless the provider says otherwise (workflows, multi-agent runs).
	set("CLAUDE_CODE_SUBAGENT_MODEL", cmp.Or(l.SubagentModel, run))
	if l.SubagentForce {
		env["CLAUDE_CODE_SUBAGENT_MODEL_FORCE"] = "1"
	}
	w := pm.ContextWindow
	if l.MaxContext > 0 {
		env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(l.MaxContext)
	} else if w > standardWindow {
		env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(w)
	}
	if mainConversation {
		env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = strconv.Itoa(standardWindow)
	} else if c := cmp.Or(l.CompactWindow, w); c > 0 {
		env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = strconv.Itoa(c)
	}
	// the main conversation switches its thinking level while it runs (apply_flag_settings, modes.go), which the
	// environment variable would win over: it takes the level from the start and the switch, not from here
	if !mainConversation {
		set("CLAUDE_CODE_EFFORT_LEVEL", cmp.Or(effort, l.Effort, pm.DefaultEffort))
	}
	set("ANTHROPIC_CUSTOM_HEADERS", l.Headers)
	if l.Proxy != "" {
		env["HTTP_PROXY"], env["HTTPS_PROXY"] = l.Proxy, l.Proxy
	}
	set("NO_PROXY", l.NoProxy)
	if l.CustomModel != "" {
		env["ANTHROPIC_CUSTOM_MODEL_OPTION"] = l.CustomModel
		set("ANTHROPIC_CUSTOM_MODEL_OPTION_NAME", l.CustomModelName)
		set("ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION", l.CustomModelDescription)
	}
	k, v, _ := strings.Cut(artifactsOff, "=")
	env[k] = v
	return env
}

// managedEnvOrder: the order `jarvisd launch` has always set a managed launcher's variables in; any other comes
// after them, sorted.
var managedEnvOrder = []string{
	"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL",
	"ANTHROPIC_CUSTOM_MODEL_OPTION", "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME", "ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "CLAUDE_CODE_DISABLE_ARTIFACT",
}

// envList is env as KEY=value entries, managedEnvOrder first. The two credentials keep the order they always had: the
// one with the key first, the emptied one after it.
func envList(env map[string]string) []string {
	order := slices.Clone(managedEnvOrder)
	if env["ANTHROPIC_AUTH_TOKEN"] == "" {
		order[1], order[2] = order[2], order[1]
	}
	rest := maps.Clone(env)
	var out []string
	for _, k := range order {
		if v, ok := rest[k]; ok {
			out = append(out, k+"="+v)
			delete(rest, k)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(rest)) {
		out = append(out, k+"="+rest[k])
	}
	return out
}

// perStartEnv: what a start decides (the main conversation keeps its 200000 compact window and switches its thinking
// level while it runs) and so never goes into settings env, which would win over the start's own environment.
var perStartEnv = []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW", "CLAUDE_CODE_EFFORT_LEVEL"}

// pinnedEnv is env without perStartEnv: the part pinned over the user's ~/.claude/settings.json (the gateway, its key,
// the models), as a settings "env".
func pinnedEnv(env map[string]string) map[string]string {
	out := maps.Clone(env)
	for _, k := range perStartEnv {
		delete(out, k)
	}
	return out
}

// launchEnvFor is what `jarvisd launch` drops from the shell and sets for a launcher (the node's clear and env lists):
// in the assistant's process (PERSONA_ASSISTANT=1), neither touches perStartEnv, so its own compact window and
// thinking level stay.
func launchEnvFor(clear, env []string, assistant bool) ([]string, []string) {
	if !assistant {
		return clear, env
	}
	clear = slices.DeleteFunc(slices.Clone(clear), func(c string) bool { return slices.Contains(perStartEnv, c) })
	env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(perStartEnv, k)
	})
	return clear, env
}
