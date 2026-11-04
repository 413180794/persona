package node

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestCookiesBecomeCDPs(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	got, skipped := toCDP([]proto.BrowserCookie{
		{Name: "sid", Value: "v1", Domain: ".Example.com", Path: "/app", Expires: 2_000_100_000, Secure: true, HTTPOnly: true, SameSite: "lax"},
		{Name: "pref", Value: "v2", Domain: "www.example.com", HostOnly: true, SameSite: "no_restriction"}, // session, None → Secure
		{Name: "old", Value: "x", Domain: "example.com", Expires: 1_000},                                   // expired
		{Name: "", Value: "x", Domain: "example.com"},                                                      // no name
		{Name: "bad", Value: "x", Domain: "exa mple.com"},
	}, now)
	if skipped != 3 || len(got) != 2 {
		t.Fatalf("%d skipped, %+v", skipped, got)
	}
	if g := got[0]; g.Domain != ".example.com" || g.Path != "/app" || !g.Secure || !g.HTTPOnly || g.SameSite != "Lax" || g.Expires != 2_000_100_000 {
		t.Fatalf("%+v", g)
	}
	if g := got[1]; g.Domain != "www.example.com" || g.Path != "/" || !g.Secure || g.SameSite != "None" || g.Expires != 0 {
		t.Fatalf("%+v", g)
	}
}

func TestCookieSitesGroupWithoutValues(t *testing.T) {
	sites := sitesOf([]rawCookie{
		{Name: "a", Domain: ".example.com", Expires: 2_000_000_000},
		{Name: "b", Domain: "www.example.com", Expires: 2_100_000_000},
		{Name: "c", Domain: "example.com", Session: true},
		{Name: "d", Domain: ".mail.example.co.uk", Expires: 1_900_000_000},
	})
	if len(sites) != 2 || sites[0].Domain != "example.co.uk" || sites[1].Domain != "example.com" || sites[1].Count != 3 {
		t.Fatalf("%+v", sites)
	}
	if !sites[1].Expires.Equal(time.Unix(2_100_000_000, 0)) {
		t.Fatalf("latest expiry %v", sites[1].Expires)
	}
	raw, _ := json.Marshal(sites)
	if strings.Contains(string(raw), `"name"`) || strings.Contains(string(raw), `"value"`) {
		t.Fatalf("a site summary carries a name or a value: %s", raw)
	}
}

func TestCookieCallsRunOneScriptAndNeverEchoValues(t *testing.T) {
	var scripts []string
	defer func(f func(context.Context, string, string) (map[string]json.RawMessage, error)) { runEgo = f }(runEgo)
	runEgo = func(_ context.Context, script, mark string) (map[string]json.RawMessage, error) {
		scripts = append(scripts, script)
		if mark != cookieMark {
			t.Fatalf("mark %q", mark)
		}
		return map[string]json.RawMessage{"set": json.RawMessage("1"), "removed": json.RawMessage("4"),
			"cookies": json.RawMessage(`[{"name":"sid","domain":".example.com","path":"/","expires":2000000000,"session":false}]`)}, nil
	}
	exp := float64(time.Now().Add(time.Hour).Unix())
	in, _ := json.Marshal(map[string]any{"cookies": []proto.BrowserCookie{{Name: "sid", Value: "s3cr3t", Domain: ".example.com", Expires: exp}, {Name: "x", Domain: "example.com", Expires: 1}}})
	out, err := browserCookies(context.Background(), "browser.cookies.import", in)
	if err != nil || out.Imported != 1 || out.Skipped != 1 || len(out.Sites) != 1 || out.Sites[0].Domain != "example.com" {
		t.Fatalf("%+v %v", out, err)
	}
	if !strings.Contains(scripts[0], "Network.setCookies") || !strings.Contains(scripts[0], "s3cr3t") || !strings.Contains(scripts[0], "task.finish") {
		t.Fatal("the import script does not set the cookies or close its space")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "s3cr3t") {
		t.Fatal("the answer carries a cookie's value")
	}
	clear, _ := json.Marshal(map[string]any{"sites": []string{".Example.com", "bad site"}})
	out, err = browserCookies(context.Background(), "browser.cookies.clear", clear)
	if err != nil || out.Removed != 4 || !strings.Contains(scripts[1], `["example.com"]`) || !strings.Contains(scripts[1], "all = false") {
		t.Fatalf("%+v %v\n%s", out, err, scripts[1])
	}
	if _, err := browserCookies(context.Background(), "browser.cookies.clear", []byte(`{}`)); err == nil {
		t.Fatal("cleared without saying what")
	}
	if _, err := browserCookies(context.Background(), "browser.cookies.import", []byte(`{"cookies":[{"name":"x","domain":"example.com","expires":1}]}`)); err == nil || len(scripts) != 2 {
		t.Fatal("an import of only expired cookies ran ego")
	}
}

func TestCookieErrorsFromEgo(t *testing.T) {
	defer func(f func(context.Context, string, string) (map[string]json.RawMessage, error)) { runEgo = f }(runEgo)
	runEgo = func(context.Context, string, string) (map[string]json.RawMessage, error) {
		return map[string]json.RawMessage{"error": json.RawMessage(`"the user is in control"`), "code": json.RawMessage(`"EGO_TASK_SPACE_USER_IN_CONTROL"`)}, nil
	}
	if _, err := browserCookies(context.Background(), "browser.cookies.list", nil); err == nil || !strings.Contains(err.Error(), "先交还") {
		t.Fatal(err)
	}
}
