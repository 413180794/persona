package i18n

import (
	"io/fs"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	for tag, want := range map[string]Lang{
		"zh": ZhHans, "zh-CN": ZhHans, "zh-Hans": ZhHans, "zh-Hant": ZhHans, "zh_TW": ZhHans, "ZH-hk": ZhHans,
		"ja": Ja, "ja-JP": Ja,
		"en": En, "en-GB": En, "fr": En, "ko-KR": En,
		"": "",
	} {
		if got := Resolve(tag); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestAcceptLanguage(t *testing.T) {
	for h, want := range map[string]Lang{
		"ja":                         Ja,
		"en-US,en;q=0.9,zh-CN;q=0.8": En,
		"zh-TW,zh;q=0.9":             ZhHans,
		"":                           "",
		"*":                          "",
		" ja-JP ; q=1, en":           Ja,
	} {
		if got := FromAcceptLanguage(h); got != want {
			t.Errorf("FromAcceptLanguage(%q) = %q, want %q", h, got, want)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en")
	if FromRequest(r) != En {
		t.Fatal("request header not read")
	}
}

func TestT(t *testing.T) {
	if got := T(En, "push.done_body"); got != "Done" {
		t.Fatalf("en = %q", got)
	}
	if got := T("", "push.done_body"); got != "已完成" {
		t.Fatalf("unknown language should be the default, got %q", got)
	}
	if got := T(En, "push.agent_asks", Args{"agent": "Codex"}); got != "Codex has a question" {
		t.Fatalf("placeholder: %q", got)
	}
	if got := T(Ja, "no.such_key"); got != "no.such_key" {
		t.Fatalf("missing key: %q", got)
	}
}

// The three languages have the same namespaces, keys (plural suffixes aside) and placeholders.
func TestCatalogsAgree(t *testing.T) {
	plural := regexp.MustCompile(`_(zero|one|two|few|many|other)$`)
	ph := regexp.MustCompile(`\{(\w+)\}`)
	shape := func(l Lang) map[string][]string {
		out := map[string][]string{}
		for k, v := range catalogs[l] {
			base := plural.ReplaceAllString(k, "")
			for _, m := range ph.FindAllStringSubmatch(v, -1) {
				if !slices.Contains(out[base], m[1]) {
					out[base] = append(out[base], m[1])
				}
			}
			if _, ok := out[base]; !ok {
				out[base] = []string{}
			}
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s %s is empty", l, k)
			}
		}
		for k := range out {
			slices.Sort(out[k])
		}
		return out
	}
	src := shape(ZhHans)
	for _, l := range Langs[1:] {
		got := shape(l)
		for k, ps := range src {
			g, ok := got[k]
			if !ok {
				t.Errorf("%s: missing %s", l, k)
				continue
			}
			if !slices.Equal(g, ps) {
				t.Errorf("%s %s placeholders %v, zh-Hans has %v", l, k, g, ps)
			}
		}
		for k := range got {
			if _, ok := src[k]; !ok {
				t.Errorf("%s: %s is not in zh-Hans", l, k)
			}
		}
	}
	names := func(l Lang) []string {
		var out []string
		entries, _ := fs.ReadDir(files, "locales/"+string(l))
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}
	for _, l := range Langs[1:] {
		if !slices.Equal(names(l), names(ZhHans)) {
			t.Errorf("%s namespaces %v, zh-Hans has %v", l, names(l), names(ZhHans))
		}
	}
	for k, v := range catalogs[ZhHans] {
		if strings.Contains(v, "Persona") {
			for _, l := range Langs[1:] {
				if s, ok := catalogs[l][k]; ok && !strings.Contains(s, "Persona") {
					t.Errorf("%s %s drops Persona: %q", l, k, s)
				}
			}
		}
	}
}
