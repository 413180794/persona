package lcuconfirm

import (
	"encoding/json"
	"strings"
)

// StateJS is the original runtime's JavaScript for one computer_state. The first getState on a fresh runtime
// returns its own guide; an app's state is that app's. query keeps only lines that contain it.
func StateJS(app, query string) string {
	var b strings.Builder
	if strings.TrimSpace(app) == "" {
		b.WriteString("const state = await cua.getState();\n")
	} else {
		b.WriteString("const app = await cua.getApp(" + jsString(app) + ");\n")
		b.WriteString("const state = typeof app.getState === \"function\" ? await app.getState() : app;\n")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		b.WriteString("return state;\n")
		return b.String()
	}
	b.WriteString("const text = typeof state === \"string\" ? state : JSON.stringify(state);\n")
	b.WriteString("const q = " + jsString(q) + ".toLowerCase();\n")
	b.WriteString("return text.split(\"\\n\").filter((line) => line.toLowerCase().includes(q)).join(\"\\n\");\n")
	return b.String()
}

// StepsJS translates computer_run steps into the original runtime's JavaScript (cua.getApp / app.click / …).
// A step the runtime does not implement fails inside the call, and the model is told the tool name.
func StepsJS(defaultApp string, steps json.RawMessage) (string, error) {
	var list []map[string]json.RawMessage
	if json.Unmarshal(steps, &list) != nil || len(list) == 0 {
		return "", errSteps
	}
	var b strings.Builder
	b.WriteString("const apps = new Map();\n")
	b.WriteString("async function appNamed(name) {\n")
	b.WriteString("  if (!name) throw new Error(\"step has no app\");\n")
	b.WriteString("  if (!apps.has(name)) apps.set(name, await cua.getApp(name));\n")
	b.WriteString("  return apps.get(name);\n")
	b.WriteString("}\n")
	for _, step := range list {
		tool, args := stepTool(step)
		if tool == "" {
			return "", errSteps
		}
		app, _ := args["app"].(string)
		if app == "" {
			app = defaultApp
		}
		delete(args, "app")
		line, ok := stepJS(tool, app, args)
		if !ok {
			return "", errSteps
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("return {ok: true};\n")
	return b.String(), nil
}

var errSteps = errString("steps must be actions the computer-use runtime can run")

type errString string

func (e errString) Error() string { return string(e) }

func stepTool(step map[string]json.RawMessage) (string, map[string]any) {
	args := map[string]any{}
	var tool string
	if raw, ok := step["tool"]; ok {
		_ = json.Unmarshal(raw, &tool)
		if a, ok := step["args"]; ok {
			_ = json.Unmarshal(a, &args)
		}
	}
	if tool == "" {
		for k, raw := range step {
			if k == "wait_for" || k == "expect" || k == "tool" || k == "args" {
				continue
			}
			tool = k
			_ = json.Unmarshal(raw, &args)
			break
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	return tool, args
}

func stepJS(tool, app string, args map[string]any) (string, bool) {
	switch tool {
	case "launch_app":
		name, _ := args["name"].(string)
		if name == "" {
			name = app
		}
		if name == "" {
			return "", false
		}
		return "await cua.launch(" + jsString(name) + ");", true
	case "click":
		return callApp(app, "click", args), true
	case "double_click":
		return callApp(app, "doubleClick", args), true
	case "right_click":
		return callApp(app, "rightClick", args), true
	case "scroll":
		return callApp(app, "scroll", args), true
	case "set_value":
		return callApp(app, "setValue", args), true
	case "type_text":
		text, _ := args["text"].(string)
		return "await (await appNamed(" + jsString(app) + ")).typeText(" + jsString(text) + ");", true
	case "press_key":
		key, _ := args["key"].(string)
		if key == "" {
			return "", false
		}
		return "await (await appNamed(" + jsString(app) + ")).pressKey(" + jsString(key) + ");", true
	case "hotkey":
		keys := hotkey(args["keys"])
		if keys == "" {
			return "", false
		}
		return "await (await appNamed(" + jsString(app) + ")).pressKey(" + jsString(keys) + ");", true
	default:
		return "throw new Error(" + jsString("unsupported step "+tool) + ");", true
	}
}

func callApp(app, method string, args map[string]any) string {
	body, _ := json.Marshal(args)
	return "await (await appNamed(" + jsString(app) + "))." + method + "(" + string(body) + ");"
}

func hotkey(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			s, _ := p.(string)
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "+")
	default:
		return ""
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
