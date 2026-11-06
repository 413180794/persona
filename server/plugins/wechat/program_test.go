package wechat

import (
	"bytes"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/exttest"
	"github.com/nwverse/persona/sdk/go/wire"
)

func manifestJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(Manifest("1.0.0", "personad-wechat"))
	if err != nil {
		t.Fatal(err)
	}
	if err := wire.ValidateJSON("Manifest", raw); err != nil {
		t.Fatalf("the manifest does not hold to schema/extension: %v", err)
	}
	return raw
}

// The program against the SDK's fake hub alone: the connector's and the channel's calls the hub makes, every
// message held to schema/extension. Not bound, there is nowhere to send to yet (cannot_send).
func TestProgramAgainstTheSDKsHub(t *testing.T) {
	m := manifestJSON(t)
	exttest.CheckConnectors(t, m, exttest.Serve(t, Program("1.0.0")))
	h := exttest.CheckChannels(t, m, exttest.Serve(t, Program("1.0.0")))
	var items []wire.ConnectorItem
	_ = json.Unmarshal(h.Init.Contributes["connectors"], &items)
	if len(items) != 1 || items[0].ID != ID || !slices.Equal(items[0].Capabilities, Capabilities) {
		t.Fatalf("initialize contributes %s", h.Init.Contributes["connectors"])
	}
	var st wire.ConnectorStatus
	if err := h.Call("connector.status", wire.ConnectorParams{Contribution: ID}, &st); err != nil ||
		st.State != wire.ConnectorStateNotSignedIn || st.Error != t2(defaultLang, "wechat.not_bound") {
		t.Fatalf("status %+v %v", st, err)
	}
	// The hub asks every connector for its tools: none, not an error.
	var tools wire.ConnectorTools
	if err := h.Call("connector.tools.list", wire.ConnectorParams{Contribution: ID}, &tools); err != nil || len(tools.Tools) != 0 {
		t.Fatalf("tools.list %+v %v", tools, err)
	}
	var e error = h.Call("connector.archive.changes", wire.ConnectorChangesParams{Contribution: ID}, nil)
	if e == nil || !strings.Contains(e.Error(), "has no") {
		t.Fatalf("an undeclared capability: %v", e)
	}
}

// The language the hub says at initialize is what the bot writes in.
func TestProgramWritesInTheHubsLanguage(t *testing.T) {
	h := exttest.StartWith(t, manifestJSON(t), exttest.Serve(t, Program("1.0.0")), func(p *wire.InitializeParams) {
		p.Hub.Locale = langEn
	})
	var st wire.ConnectorStatus
	if err := h.Call("connector.status", wire.ConnectorParams{Contribution: ID}, &st); err != nil || st.Error != "WeChat isn't bound yet" {
		t.Fatalf("status %+v %v", st, err)
	}
}

func t2(l, key string) string { return t(l, key) }

var tr = t

func TestLocalesHaveTheSameKeys(t *testing.T) {
	want := slices.Sorted(maps.Keys(catalogs[langZhHans]))
	for _, l := range []string{langEn, langJa} {
		if got := slices.Sorted(maps.Keys(catalogs[l])); !slices.Equal(got, want) {
			t.Errorf("%s: keys %v, zh-Hans has %v", l, got, want)
		}
	}
	if langOf("") != defaultLang || langOf("ko") != langEn || langOf(langJa) != langJa {
		t.Error("langOf")
	}
	// The same {placeholders} in every language.
	ph := regexp.MustCompile(`\{\w+\}`)
	for key, zh := range catalogs[langZhHans] {
		want := slices.Sorted(slices.Values(ph.FindAllString(zh, -1)))
		for _, l := range []string{langEn, langJa} {
			if got := slices.Sorted(slices.Values(ph.FindAllString(catalogs[l][key], -1))); !slices.Equal(got, want) {
				t.Errorf("%s %s: placeholders %v, zh-Hans has %v", l, key, got, want)
			}
		}
	}
	if got := t2(langEn, "no.such.key"); got != "no.such.key" {
		t.Errorf("a missing key: %q", got)
	}
	if got := tr(langEn, "wechat.account", "id", "123456"); got != "WeChat user 123456" {
		t.Errorf("args: %q", got)
	}
}

func TestBuildLine(t *testing.T) {
	for _, c := range []struct {
		b    Build
		want string
	}{
		{Build{Version: "dev"}, "channel dev, build 0"},
		{Build{Channel: "release", Number: "202611061230"}, "channel release, build 202611061230"},
		{Build{Channel: "release", Number: "2026"}, "channel dev, build 0"},
		{Build{Channel: "release", Number: "202613061230"}, "channel dev, build 0"},
		{Build{Channel: "docker", Number: "202611061230"}, "channel docker, build 202611061230"},
		{Build{Channel: "docker"}, "channel docker, build 0"},
		{Build{Channel: "nightly", Number: "202611061230"}, "channel dev, build 202611061230"},
	} {
		if got := c.b.Line(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.b, got, c.want)
		}
	}
}

func TestCommandLine(t *testing.T) {
	b := Build{Version: "dev"}
	cases := []struct {
		name     string
		args     []string
		terminal bool
		code     int
		done     bool
		stdout   string
		stderr   string
	}{
		{"hub pipe", nil, false, 0, false, "", ""},
		{"by hand in a terminal", nil, true, 2, true, "", "usage: personad-wechat"},
		{"version", []string{"version"}, true, 0, true, "dev", ""},
		{"version -v", []string{"version", "-v"}, false, 0, true, "channel ", ""},
		{"--help", []string{"--help"}, true, 0, true, "personad-wechat version -v", ""},
		{"-h", []string{"-h"}, false, 0, true, "usage: personad-wechat", ""},
		{"unknown", []string{"serve"}, false, 2, true, "", `unknown argument "serve"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, done := CommandLine("personad-wechat", b, c.args, c.terminal, &stdout, &stderr)
			if code != c.code || done != c.done {
				t.Fatalf("got (%d, %v), want (%d, %v)", code, done, c.code, c.done)
			}
			if !strings.Contains(stdout.String(), c.stdout) || (c.stdout == "" && stdout.Len() > 0) {
				t.Errorf("stdout = %q, want %q", stdout.String(), c.stdout)
			}
			if !strings.Contains(stderr.String(), c.stderr) || (c.stderr == "" && stderr.Len() > 0) {
				t.Errorf("stderr = %q, want %q", stderr.String(), c.stderr)
			}
		})
	}
	var stdout bytes.Buffer
	CommandLine("personad-wechat", Build{Version: "v1", Channel: "release", Number: "202611061230"}, []string{"version", "-v"}, false, &stdout, &bytes.Buffer{})
	if stdout.String() != "v1\nchannel release, build 202611061230\n" {
		t.Errorf("version -v = %q", stdout.String())
	}
	stdout.Reset()
	CommandLine("jarvisd-wechat", b, []string{"--help"}, false, &stdout, &bytes.Buffer{})
	if !strings.Contains(stdout.String(), "jarvisd-wechat version -v") || strings.Contains(stdout.String(), "personad-wechat") {
		t.Errorf("the compat name's usage = %q", stdout.String())
	}
}
