package extension

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseMarket(t *testing.T) {
	for in, want := range map[string]string{
		"github:example/plugins":                      "github:example/plugins",
		"github:example/plugins@release":              "github:example/plugins@release",
		"https://github.com/example/plugins":          "github:example/plugins",
		"https://github.com/example/plugins/tree/dev": "github:example/plugins@dev",
		"https://example.com/plugins":                 "https://example.com/plugins/persona-extensions.json",
		"https://example.com/plugins/":                "https://example.com/plugins/persona-extensions.json",
		"https://example.com/v100/market.json#x":      "https://example.com/v100/market.json",
		" https://example.com ":                       "https://example.com/persona-extensions.json",
	} {
		s, err := ParseMarket(in)
		if err != nil || s.String() != want {
			t.Errorf("%q: %q %v", in, s.String(), err)
		}
	}
	for _, in := range []string{"http://example.com/plugins", "github:example", "github:../x/y", "ftp://example.com", "https://user:pw@example.com/", "example.com"} {
		if _, err := ParseMarket(in); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
	a, _ := ParseMarket("github:example/plugins")
	b, _ := ParseMarket("https://github.com/example/plugins")
	if MarketID(a) != MarketID(b) || len(MarketID(a)) != 10 {
		t.Fatal(MarketID(a), MarketID(b))
	}
}

func TestParseIndex(t *testing.T) {
	entry := func(change func(e map[string]any)) map[string]any {
		var m map[string]any
		_ = json.Unmarshal([]byte(exampleManifest), &m)
		e := map[string]any{"manifest": m, "tags": []string{"笔记"}, "packages": []any{
			map[string]any{"platforms": []string{"linux-amd64"}, "url": "dist/notes-linux.tar.gz", "sha256": strings.Repeat("a", 64)},
			map[string]any{"url": "https://example.com/notes.tar.gz", "sha256": strings.Repeat("b", 64)}}}
		if change != nil {
			change(e)
		}
		return e
	}
	b, _ := json.Marshal(map[string]any{"market_version": 1, "name": "Example", "homepage": "http://example.com", "plugins": []any{
		entry(nil),
		entry(nil), // twice
		entry(func(e map[string]any) { e["packages"] = []any{} }),
		entry(func(e map[string]any) {
			e["packages"] = []any{map[string]any{"url": "http://example.com/x.tar.gz", "sha256": strings.Repeat("a", 64)}}
		}),
		entry(func(e map[string]any) { e["packages"] = []any{map[string]any{"url": "x.tar.gz", "sha256": "short"}} }),
		entry(func(e map[string]any) { e["manifest"] = map[string]any{"id": "x"} }),
	}})
	ix, warnings, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Extensions) != 1 || len(warnings) != 5 || ix.Homepage != "" {
		t.Fatalf("%d %v %q", len(ix.Extensions), warnings, ix.Homepage)
	}
	e := ix.Extensions[0]
	if e.Parsed.ID != "example-notes" || e.PackageFor("linux-amd64").URL != "dist/notes-linux.tar.gz" || e.PackageFor("darwin-arm64").URL != "https://example.com/notes.tar.gz" {
		t.Fatalf("%+v", e)
	}
	for _, bad := range []string{`{"market_version": 2, "name": "x", "plugins": []}`, `{"market_version": 1, "plugins": []}`, `[]`} {
		if _, _, err := ParseIndex([]byte(bad)); err == nil {
			t.Errorf("%s: parsed", bad)
		}
	}
	if FromMarket(MarketOrigin("abc", "example-notes")) != "abc" || FromMarket("dir:/tmp/x") != "" {
		t.Fatal("origin")
	}
}
