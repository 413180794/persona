package extension

import (
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestCheckEnginesCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, engines string
		build         int64
		key           string
		warning       bool
	}{
		{name: "unrestricted", build: 100},
		{name: "development", engines: ">=100 <200", warning: true},
		{name: "too old", engines: ">=100", build: 99, key: "extension-version.hub_too_old"},
		{name: "minimum included", engines: ">=100", build: 100},
		{name: "newer allowed", engines: ">=100", build: 200},
		{name: "bounded too old", engines: ">=100 <200", build: 99, key: "extension-version.hub_too_old"},
		{name: "reordered too old", engines: "<200 >=100", build: 99, key: "extension-version.hub_too_old"},
		{name: "bounded allowed", engines: ">=100 <200", build: 199},
		{name: "maximum excluded", engines: ">=100 <200", build: 200, key: "extension-version.hub_incompatible"},
		{name: "upper only", engines: "<200", build: 200, key: "extension-version.hub_incompatible"},
		{name: "upper allows older", engines: "<200", build: 100},
		{name: "conflicting bounds", engines: ">=200 <100", build: 150, key: "extension-version.hub_incompatible"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{}
			m.Engines.Persona = tc.engines
			warning, err := CheckEngines(m, tc.build)
			if (warning != "") != tc.warning {
				t.Fatalf("warning = %q, want warning: %v", warning, tc.warning)
			}
			if tc.key == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var msg *i18n.Message
			if !errors.As(err, &msg) || msg.Key != tc.key {
				t.Fatalf("error = %v, want %s", err, tc.key)
			}
			if msg.Args["required"] != tc.engines || msg.Args["current"] != tc.build {
				t.Fatalf("build details lost: %v", msg.Args)
			}
		})
	}
}

func TestCheckEnginesUpdateGuidanceLanguages(t *testing.T) {
	m := &Manifest{}
	m.Engines.Persona = ">=202612010000"
	_, err := CheckEngines(m, 202611041200)
	if err == nil {
		t.Fatal("an older hub must be rejected")
	}
	for _, tc := range []struct {
		lang i18n.Lang
		want string
	}{
		{i18n.ZhHans, "这个扩展需要更新版本的 hub，请先更新 hub（需要 >=202612010000，现在是 202611041200）"},
		{i18n.En, "This extension needs a newer hub. Update the hub first (required: >=202612010000, current: 202611041200)."},
		{i18n.Ja, "この拡張機能には新しいバージョンのハブが必要です。先にハブを更新してください（必要なビルド：>=202612010000、現在：202611041200）"},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			if got := i18n.Localize(err, tc.lang); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
	if got := err.Error(); !strings.HasPrefix(got, "这个扩展需要更新版本的 hub，请先更新 hub") {
		t.Fatalf("log/CLI message must remain Chinese: %q", got)
	}
}

func TestCheckEnginesInvalidRange(t *testing.T) {
	m := &Manifest{}
	m.Engines.Persona = ">=invalid"
	for _, build := range []int64{0, 100} {
		warning, err := CheckEngines(m, build)
		var msg *i18n.Message
		if warning != "" || err == nil || errors.As(err, &msg) {
			t.Fatalf("build %d: invalid range must remain a parse error, got %q, %v", build, warning, err)
		}
	}
}

func TestLoadedEngineProblemLanguages(t *testing.T) {
	r := NewRegistry(t.TempDir(), nil, nil, 202611040000)
	p := r.fromRow(Record{ID: "example-notes", Source: SourceInstalled, Manifest: []byte(exampleManifest), Remote: &Remote{}})
	for _, loaded := range []*Extension{p, snapshotExtension(p)} {
		if got := loaded.ProblemText(i18n.En); !strings.HasPrefix(got, "This extension needs a newer hub. Update the hub first") {
			t.Fatalf("loaded problem lost its translation: %q", got)
		}
		if got := loaded.ProblemText(i18n.ZhHans); got != loaded.Problem {
			t.Fatalf("default problem changed: %q != %q", got, loaded.Problem)
		}
	}
	p.Problem = "package unavailable"
	if got := p.ProblemText(i18n.En); got != p.Problem {
		t.Fatalf("a replacement problem must not show the old engine error: %q", got)
	}
	plain := &Extension{Problem: "manifest unavailable"}
	if got := plain.ProblemText(i18n.En); got != plain.Problem {
		t.Fatalf("untranslated problem changed: %q", got)
	}
}
