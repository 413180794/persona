package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// completeLarkContextModel uses a single binding chosen by the user, including when the request names candidates.
func (s *Service) completeLarkContextModel(ctx context.Context, who string, c checkedCall) (any, error) {
	ctx = withSettingsMemo(ctx)
	b, err := s.resolveModel(ctx, larkContextModelPurposeID)
	if err != nil {
		return nil, unavailableLarkContextModel("the model purpose could not be resolved")
	}
	switch b.Provider {
	case "legacy.ai", "legacy.triage", "legacy.judge", "legacy.kb":
	default:
		name, ok := strings.CutPrefix(b.Provider, "launcher.")
		if !ok {
			return nil, unavailableLarkContextModel("the configured provider is unavailable")
		}
		stored, known := s.storedLaunchersKnown(ctx)
		if !known {
			return nil, unavailableLarkContextModel("the configured provider could not be read")
		}
		ctx = withManagedSnapshot(ctx, stored)
		i := slices.IndexFunc(stored, func(l storedLauncher) bool { return l.Name == name })
		if i < 0 {
			return nil, s.larkContextLocalBinding(ctx, name, b)
		}
		if l := stored[i]; l.Kind != "api" && !larkContextManagedAPI(l) {
			return nil, unavailableLarkContextModel("the configured managed launcher requires settings the direct API cannot use")
		}
	}
	config, err := s.apiConfig(ctx, b)
	if err != nil || config.RunLauncher != "" || !config.Configured() {
		return nil, unavailableLarkContextModel("the configured direct API could not be used")
	}
	return s.runConfiguredModelCall(ctx, larkContextModelPurposeID, who, b.Provider, config, c, true)
}

// A managed CLI's environment, arguments or transport can change the account or model that answers.
// Only the existing server-owned Messages connection can serve such a provider directly.
func larkContextManagedAPI(l storedLauncher) bool {
	return messagesAPI(l.API) && l.BaseURL != "" && l.KeySealed != "" &&
		l.Proxy == "" && l.NoProxy == "" && l.HeadersSealed == "" && l.Headers == "" && !l.HasHeaders &&
		l.EnvSealed == "" && l.Env == "" && !l.HasEnv && len(l.Args) == 0
}

func (s *Service) larkContextLocalBinding(ctx context.Context, name string, b ModelBinding) error {
	if !managedLauncherName.MatchString(name) || b.ConfigDir != "" {
		return unavailableLarkContextModel("the configured launcher cannot be used locally")
	}
	seen := s.zshrcLaunchers(ctx, false)
	var found LauncherView
	if name == "claude" {
		found, _ = s.builtinClaude(ctx, seen)
		found.Agent = "claude"
	} else {
		views := s.launcherViewsOf(ctx, seen)
		if i := slices.IndexFunc(views, func(v LauncherView) bool { return v.Name == name }); i >= 0 {
			found = views[i]
		}
	}
	runtime := viewAgent(found)
	if len(found.Machines) == 0 || found.Source == "persona" || !slices.Contains([]string{"agy", "codex", "traex", "claude"}, runtime) {
		return unavailableLarkContextModel("the configured local launcher is unavailable or unsupported")
	}
	if name != runtime || found.Source == "path" || (found.BaseURL != "" && found.Source != "agy") {
		return unavailableLarkContextModel("the configured launcher cannot be reproduced by the isolated local runtime")
	}
	model := cmp.Or(b.Model, found.Model)
	if model == "" {
		machine := cmp.Or(s.settings(ctx).MachineID, found.Machines[0])
		if slices.Contains(found.Machines, machine) {
			_, model = s.launcherModelResolution(ctx, machine, name)
		}
	}
	if model == "" {
		return unavailableLarkContextModel("the configured launcher did not report its default model")
	}
	validModel := proto.ValidModelID(model) && !strings.Contains(model, "://")
	validEffort := b.Effort == "" || b.Effort == "off" || slices.Contains(proto.EffortWords, b.Effort)
	if !validModel || !validEffort {
		return unavailableLarkContextModel("the configured launcher model or effort cannot be used locally")
	}
	data, _ := json.Marshal(assistantwire.ModelLocalLauncherRequired{Binding: assistantwire.ModelLocalLauncherBinding{
		Provider: b.Provider, Runtime: runtime, Model: model, Effort: b.Effort,
	}})
	return &rpc.Error{Code: assistantwire.CodeModelLocalLauncherRequired, Message: "model.complete: the configured model requires its local launcher", Data: data}
}

func unavailableLarkContextModel(reason string) *rpc.Error {
	return &rpc.Error{Code: assistantwire.CodeModelUnavailable, Message: "model.complete: " + reason}
}

func (s *Service) runConfiguredModelCall(ctx context.Context, purpose, who, provider string, config ai.Config, c checkedCall, singleRequest bool) (any, error) {
	var usage assistantwire.ModelUsage
	started := time.Now()
	client := s.countedClient(purpose, "", provider, config)
	var out assistantwire.ModelCompleteResult
	var outcome string
	var err error
	if singleRequest {
		out, outcome, err = s.attemptLarkContextModel(ctx, client.WithSingleRequest(), c, &usage)
		out.Provider, out.Effort = provider, config.ReasoningEffort
	} else {
		out, outcome, err = s.attemptModel(ctx, client, c, c.timeout, &usage)
	}
	s.logf("assistant: %s model call%s on %s: %s in %s, %d request(s), %d input + %d output tokens (cache read %d, written %d)",
		who, labelPart(c.p.Label), config.Model, outcome, time.Since(started).Round(time.Millisecond), usage.Requests,
		usage.InputTokens, usage.OutputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) attemptLarkContextModel(ctx context.Context, client *ai.Client, c checkedCall, usage *assistantwire.ModelUsage) (assistantwire.ModelCompleteResult, string, error) {
	// The prompt still asks for the schema. Validate the first response without sending a correction request.
	schema := c.schema
	c.schema = nil
	out, outcome, err := s.attemptModel(ctx, client, c, c.timeout, usage)
	if re, ok := err.(*rpc.Error); ok {
		// Upstream and transport diagnostics can include credentials, addresses, or local file paths.
		switch re.Code {
		case assistantwire.CodeModelFailed:
			err = &rpc.Error{Code: re.Code, Message: "model.complete: the configured direct API request failed"}
		case assistantwire.CodeModelUnavailable:
			err = unavailableLarkContextModel("the configured direct API could not be used")
		}
	}
	if err != nil || schema == nil {
		return out, outcome, err
	}
	value, err := decodeReply(out.Text)
	if err != nil || validateSchema(schema, schema, value, "$", 0) != "" {
		const problem = "the reply does not match json_schema"
		data, _ := json.Marshal(assistantwire.ModelInvalidOutput{Problem: problem, Usage: *usage})
		return assistantwire.ModelCompleteResult{}, "invalid output",
			&rpc.Error{Code: assistantwire.CodeInvalidOutput, Message: "model.complete: " + problem, Data: data}
	}
	out.JSON, _ = json.Marshal(value)
	return out, outcome, nil
}
