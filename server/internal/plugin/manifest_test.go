package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// exampleManifest is the plan's example (docs/plan-plugins.md「三」), a made-up notes service.
const exampleManifest = `{
  "manifest_version": 1,
  "id": "example-notes",
  "name": "Example Notes",
  "version": "1.2.0",
  "description": "notes into the knowledge base",
  "publisher": "example",
  "homepage": "https://example.com/persona-plugin",
  "engines": {"persona": ">=202611050000", "plugin_protocol": 1},
  "runtime": {"command": {"linux-amd64": "bin/notes-linux", "darwin-arm64": "bin/notes-mac"}, "args": ["serve"]},
  "activation_events": ["on_startup"],
  "contributes": {"connectors": [{"id": "examplenotes", "name": "Example Notes", "capabilities": ["status", "tools"], "hosts": ["notes.example.com"], "poll_seconds": 600}]},
  "configuration": {"type": "object", "properties": {
    "base_url": {"type": "string", "title": "服务地址", "format": "uri", "default": "https://notes.example.com"},
    "folders": {"type": "array", "title": "文件夹", "items": {"type": "string"}},
    "include_shared": {"type": "boolean", "default": false}
  }, "required": ["base_url"]},
  "secrets": [{"name": "api_key", "title": "API Key", "required": true}],
  "permissions": {"network": ["notes.example.com"]}
}`

// with is the example manifest with one change made to its decoded form.
func with(t *testing.T, change func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(exampleManifest), &m); err != nil {
		t.Fatal(err)
	}
	change(m)
	b, _ := json.Marshal(m)
	return b
}

func TestParseExample(t *testing.T) {
	m, err := Parse([]byte(exampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "example-notes" || m.Kind() != "connectors" || len(m.Connectors()) != 1 || m.Connectors()[0].PollSeconds != 600 {
		t.Fatalf("%+v", m)
	}
	if cmd, ok := m.Runtime.Command.For("linux-amd64"); !ok || cmd != "bin/notes-linux" {
		t.Fatal(cmd)
	}
	if _, ok := m.Runtime.Command.For("windows-amd64"); ok {
		t.Fatal("no windows program")
	}
	if got := m.Contributions(); len(got["connectors"]) != 1 || got["connectors"][0] != "examplenotes" {
		t.Fatal(got)
	}
	if w := m.Lint(); len(w) != 0 {
		t.Fatal(w)
	}
}

func TestParseRefuses(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"unknown top-level field": func(m map[string]any) { m["permission"] = map[string]any{} },
		"manifest version":        func(m map[string]any) { m["manifest_version"] = 2 },
		"bad id":                  func(m map[string]any) { m["id"] = "Bad ID" },
		"id starting with -":      func(m map[string]any) { m["id"] = "-x" },
		"no name":                 func(m map[string]any) { m["name"] = " " },
		"version":                 func(m map[string]any) { m["version"] = "1.2" },
		"homepage http":           func(m map[string]any) { m["homepage"] = "http://example.com" },
		"engines range":           func(m map[string]any) { m["engines"] = map[string]any{"persona": "~1", "plugin_protocol": 1} },
		"protocol":                func(m map[string]any) { m["engines"] = map[string]any{"plugin_protocol": 9} },
		"absolute command":        func(m map[string]any) { m["runtime"].(map[string]any)["command"] = "/usr/bin/x" },
		"command outside":         func(m map[string]any) { m["runtime"].(map[string]any)["command"] = "../x" },
		"command unclean":         func(m map[string]any) { m["runtime"].(map[string]any)["command"] = "bin/../../x" },
		"platform key":            func(m map[string]any) { m["runtime"].(map[string]any)["command"] = map[string]any{"linux": "x"} },
		"rpc value":               func(m map[string]any) { m["runtime"].(map[string]any)["rpc"] = "tcp" },
		"no way to talk":          func(m map[string]any) { m["runtime"].(map[string]any)["rpc"] = "none" },
		"hub's env": func(m map[string]any) {
			m["runtime"].(map[string]any)["env"] = map[string]any{"PERSONA_HUB_TOKEN": "x"}
		},
		"activation":           func(m map[string]any) { m["activation_events"] = []any{"onStartup"} },
		"on_http without http": func(m map[string]any) { m["activation_events"] = []any{"on_http"} },
		"nothing contributed":  func(m map[string]any) { m["contributes"] = map[string]any{} },
		"connector id": func(m map[string]any) {
			m["contributes"] = map[string]any{"connectors": []any{map[string]any{"id": "Ex", "name": "x", "capabilities": []any{"status"}}}}
		},
		"connector field": func(m map[string]any) {
			m["contributes"] = map[string]any{"connectors": []any{map[string]any{"id": "ex", "name": "x", "capabilities": []any{"status"}, "pol": 1}}}
		},
		"connector twice": func(m map[string]any) {
			c := map[string]any{"id": "ex", "name": "x", "capabilities": []any{"status"}}
			m["contributes"] = map[string]any{"connectors": []any{c, c}}
		},
		"connectors without rpc": func(m map[string]any) {
			m["runtime"] = map[string]any{"command": "bin/x", "rpc": "none", "http": true}
			m["activation_events"] = []any{"on_startup"}
		},
		"voice without http": func(m map[string]any) {
			m["contributes"] = map[string]any{"voice": []any{map[string]any{"id": "voice"}}}
		},
		"connectors without runtime": func(m map[string]any) { delete(m, "runtime"); delete(m, "activation_events") },
		"secret name":                func(m map[string]any) { m["secrets"] = []any{map[string]any{"name": "Key", "title": "k"}} },
		"secret twice": func(m map[string]any) {
			s := map[string]any{"name": "k", "title": "k"}
			m["secrets"] = []any{s, s}
		},
		"secret without title": func(m map[string]any) { m["secrets"] = []any{map[string]any{"name": "k"}} },
		"network with scheme":  func(m map[string]any) { m["permissions"] = map[string]any{"network": []any{"https://example.com"}} },
		"unknown permission":   func(m map[string]any) { m["permissions"] = map[string]any{"files": true} },
		"schema keyword": func(m map[string]any) {
			m["configuration"] = map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string", "$ref": "#"}}}
		},
		"something after": nil,
	}
	for name, change := range cases {
		var data []byte
		if change == nil {
			data = []byte(exampleManifest + "{}")
		} else {
			data = with(t, change)
		}
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestLintWarnsUnknownPointAndSecretLookingConfig(t *testing.T) {
	m, err := Parse(with(t, func(m map[string]any) {
		m["contributes"].(map[string]any)["kb_sources"] = []any{map[string]any{"id": "q"}}
		m["configuration"] = map[string]any{"type": "object", "properties": map[string]any{"api_token": map[string]any{"type": "string"}}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Join(m.Lint(), "\n")
	if !strings.Contains(w, "contributes.kb_sources") || !strings.Contains(w, "api_token") {
		t.Fatal(w)
	}
	if _, ok := m.Contributions()["kb_sources"]; ok {
		t.Fatal("an unknown point does not take effect")
	}
}

func TestRuntimeDefaults(t *testing.T) {
	m, err := Parse(with(t, func(m map[string]any) { delete(m, "activation_events") }))
	if err != nil {
		t.Fatal(err)
	}
	if a := m.Activation(); len(a) != 1 || a[0] != OnCall {
		t.Fatal(a)
	}
	if !m.Runtime.SpeaksRPC() {
		t.Fatal("stdio is the default")
	}
	// A program serving HTTP only must run on_startup: the hub has no other way to know it is up.
	if _, err := Parse(with(t, func(m map[string]any) {
		m["runtime"] = map[string]any{"command": "bin/x", "rpc": "none", "http": true}
		delete(m, "activation_events")
		m["contributes"] = map[string]any{"voice": []any{map[string]any{"id": "voice"}}}
	})); err == nil || !strings.Contains(err.Error(), "on_startup") {
		t.Fatal(err)
	}
}

func TestDigestAndCovers(t *testing.T) {
	a, _ := Parse([]byte(exampleManifest))
	b, _ := Parse(with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"network": []any{"notes.example.com", "notes.example.com"}}
		m["version"] = "2.0.0"
	}))
	if Digest(a) != Digest(b) {
		t.Fatal("the same permissions (hosts once each) and code: the same digest, whatever the version")
	}
	c, _ := Parse(with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"network": []any{"notes.example.com"}, "hub_api": true}
	}))
	if Digest(a) == Digest(c) {
		t.Fatal("more permissions, another digest")
	}
	if !c.Permissions.Covers(a.Permissions) || a.Permissions.Covers(c.Permissions) {
		t.Fatal("covers")
	}
	lines := Explain(c)
	keys := []string{}
	for _, l := range lines {
		keys = append(keys, l.Key)
	}
	if strings.Join(keys, ",") != "runs_code,network,hub_api,secrets" {
		t.Fatal(keys)
	}
}

func TestVersions(t *testing.T) {
	order := []string{"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0", "1.0.1", "1.10.0", "2.0.0"}
	for i := 1; i < len(order); i++ {
		if !NewerVersion(order[i], order[i-1]) || NewerVersion(order[i-1], order[i]) {
			t.Errorf("%s > %s", order[i], order[i-1])
		}
	}
	for _, bad := range []string{"1", "1.0", "01.0.0", "1.0.0+build", "v1.0.0"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}

func TestRanges(t *testing.T) {
	r, err := ParseRange(">=100 <200")
	if err != nil {
		t.Fatal(err)
	}
	for build, want := range map[int64]bool{99: false, 100: true, 199: true, 200: false} {
		if r.Allows(build) != want {
			t.Errorf("%d", build)
		}
	}
	for _, bad := range []string{"", "=100", "<=100", ">100", ">=x"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	m, _ := Parse([]byte(exampleManifest))
	if _, err := CheckEngines(m, 202611040000); err == nil {
		t.Fatal("too old a hub")
	}
	if w, err := CheckEngines(m, 0); err != nil || w == "" {
		t.Fatal("a development build runs it, with a warning")
	}
	if w, err := CheckEngines(m, 202611060000); err != nil || w != "" {
		t.Fatal(w, err)
	}
}

func TestTargeted(t *testing.T) {
	b, err := Targeted(struct {
		Cursor string `json:"cursor"`
	}{"c1"}, "notes")
	if err != nil || string(b) != `{"contribution":"notes","cursor":"c1"}` {
		t.Fatal(string(b), err)
	}
	if b, _ := Targeted(struct{}{}, ""); string(b) != `{}` {
		t.Fatal(string(b))
	}
	if b, _ := Targeted(nil, "notes"); string(b) != `{"contribution":"notes"}` {
		t.Fatal(string(b))
	}
	if _, err := Targeted([]int{1}, "notes"); err == nil {
		t.Fatal("not an object")
	}
}

func TestQuotaAndProviders(t *testing.T) {
	quota := func(perm bool, item map[string]any) []byte {
		return with(t, func(m map[string]any) {
			m["contributes"] = map[string]any{"quota": []any{item}}
			m["permissions"] = map[string]any{"launcher_keys": perm}
		})
	}
	ok := map[string]any{"id": "example-gw", "name": "Example 网关", "hosts": []any{"gw.example.com", "*.example.net"}}
	m, err := Parse(quota(true, ok))
	if err != nil {
		t.Fatal(err)
	}
	if q := m.Quotas(); len(q) != 1 || q[0].Hosts[1] != "*.example.net" || m.Kind() != "quota" {
		t.Fatalf("%+v", q)
	}
	lines := Explain(m)
	if lines[len(lines)-2].Key != "launcher_keys" {
		t.Fatal(lines)
	}
	if _, err := Parse(quota(false, ok)); err == nil || !strings.Contains(err.Error(), "launcher_keys") {
		t.Fatal("quota without launcher_keys:", err)
	}
	if _, err := Parse(quota(true, map[string]any{"id": "x", "name": "x", "hosts": []any{"https://x"}})); err == nil {
		t.Fatal("a host with a scheme")
	}
	provider := func(item map[string]any, runtime bool) []byte {
		return with(t, func(m map[string]any) {
			m["contributes"] = map[string]any{"providers": []any{item}}
			delete(m, "permissions")
			if !runtime {
				delete(m, "runtime")
				delete(m, "activation_events")
				delete(m, "engines")
			}
		})
	}
	p := map[string]any{"id": "example-ai", "name": "Example AI", "kind": "api", "api": "responses", "base_url": "https://api.example.com/v1",
		"models": []any{map[string]any{"id": "example-large", "name": "Large"}, map[string]any{"id": "example-small"}}, "model": "example-large"}
	m, err = Parse(provider(p, false))
	if err != nil {
		t.Fatal("a declarative provider:", err)
	}
	if pr := m.Providers(); len(pr) != 1 || len(pr[0].Models) != 2 || m.Runtime != nil {
		t.Fatalf("%+v", pr)
	}
	for name, change := range map[string]func(map[string]any){
		"base_url":        func(i map[string]any) { i["base_url"] = "ftp://x" },
		"kind":            func(i map[string]any) { i["kind"] = "proxy" },
		"api":             func(i map[string]any) { i["api"] = "chat" },
		"model twice":     func(i map[string]any) { i["models"] = []any{map[string]any{"id": "a"}, map[string]any{"id": "a"}} },
		"dynamic, no rpc": func(i map[string]any) { i["dynamic_models"] = true },
	} {
		item := map[string]any{}
		for k, v := range p {
			item[k] = v
		}
		change(item)
		if _, err := Parse(provider(item, false)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	p["dynamic_models"] = true
	if _, err := Parse(provider(p, true)); err != nil {
		t.Fatal("dynamic models with a program:", err)
	}
}

func TestAPIAndAssistantPoints(t *testing.T) {
	m, err := Parse(with(t, func(m map[string]any) {
		m["runtime"].(map[string]any)["http"] = true
		m["contributes"] = map[string]any{
			"api":       []any{map[string]any{"id": "feed", "prefix": "examplefeed", "features": []any{"example_feed"}, "events": []any{"examplefeed"}}},
			"assistant": []any{map[string]any{"id": "feed", "skills": []any{"skills/feed"}, "instructions": "assistant.md"}}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if a := m.APIs(); len(a) != 1 || a[0].Prefix != "examplefeed" || len(m.Assistants()) != 1 {
		t.Fatalf("%+v", a)
	}
	for name, change := range map[string]func(m map[string]any){
		"api without http": func(m map[string]any) {
			m["contributes"] = map[string]any{"api": []any{map[string]any{"id": "x", "prefix": "x"}}}
		},
		"api not on startup": func(m map[string]any) {
			m["runtime"].(map[string]any)["http"] = true
			m["activation_events"] = []any{"on_call"}
			m["contributes"] = map[string]any{"api": []any{map[string]any{"id": "x", "prefix": "x"}}}
		},
		"prefix with a slash": func(m map[string]any) {
			m["runtime"].(map[string]any)["http"] = true
			m["contributes"] = map[string]any{"api": []any{map[string]any{"id": "x", "prefix": "a/b"}}}
		},
		"feature name": func(m map[string]any) {
			m["runtime"].(map[string]any)["http"] = true
			m["contributes"] = map[string]any{"api": []any{map[string]any{"id": "x", "prefix": "x", "features": []any{"Bad-Name"}}}}
		},
		"skill outside": func(m map[string]any) {
			m["contributes"] = map[string]any{"assistant": []any{map[string]any{"id": "x", "skills": []any{"../skills"}}}}
		},
		"instructions not md": func(m map[string]any) {
			m["contributes"] = map[string]any{"assistant": []any{map[string]any{"id": "x", "instructions": "a.txt"}}}
		},
		"assistant empty": func(m map[string]any) {
			m["contributes"] = map[string]any{"assistant": []any{map[string]any{"id": "x"}}}
		},
	} {
		if _, err := Parse(with(t, change)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestUIAndEventsPoints(t *testing.T) {
	m, err := Parse(with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"launcher_keys": true}
		m["contributes"] = map[string]any{
			"ui": []any{map[string]any{"id": "home", "title": "概况", "placement": "plugin_page"},
				map[string]any{"id": "panel", "title": "面板", "placement": "launcher_section", "hosts": []any{"gw.example.com"}}},
			"events": []any{map[string]any{"id": "e", "subscribe": []any{"agent.finished", "inbox.changed"}}}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.UIs()) != 2 || len(m.Topics()) != 2 {
		t.Fatalf("%+v %v", m.UIs(), m.Topics())
	}
	if !strings.Contains(fmt.Sprint(Explain(m)), "收到 hub 的事件") {
		t.Fatal("the events are explained")
	}
	if Digest(m) == Digest(mustParse(t)(with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"launcher_keys": true}
		m["contributes"] = map[string]any{"events": []any{map[string]any{"id": "e", "subscribe": []any{"agent.finished"}}}}
	}))) {
		t.Fatal("the topics are in the digest")
	}
	for name, change := range map[string]func(m map[string]any){
		"placement": func(m map[string]any) {
			m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "x", "title": "x", "placement": "sidebar"}}}
		},
		"section without hosts": func(m map[string]any) {
			m["permissions"] = map[string]any{"launcher_keys": true}
			m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "x", "title": "x", "placement": "launcher_section"}}}
		},
		"section without keys": func(m map[string]any) {
			m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "x", "title": "x", "placement": "launcher_section", "hosts": []any{"gw.example.com"}}}}
		},
		"ui without rpc": func(m map[string]any) {
			m["runtime"].(map[string]any)["rpc"] = "none"
			m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "x", "title": "x", "placement": "plugin_page"}}}
		},
		"unknown topic": func(m map[string]any) {
			m["contributes"] = map[string]any{"events": []any{map[string]any{"id": "e", "subscribe": []any{"mail.sent"}}}}
		},
		"events not on startup": func(m map[string]any) {
			m["activation_events"] = []any{"on_call"}
			m["contributes"] = map[string]any{"events": []any{map[string]any{"id": "e", "subscribe": []any{"agent.finished"}}}}
		},
	} {
		if _, err := Parse(with(t, change)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func mustParse(t *testing.T) func(b []byte) *Manifest {
	return func(b []byte) *Manifest {
		m, err := Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
}
