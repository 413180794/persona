package extension

import (
	"slices"
	"strings"
	"testing"
)

// auth_relay: the permission is shown and asked again on an upgrade that adds it; the contribution needs it and a
// program that speaks the protocol, and names exact hosts and paths.
func TestAuthRelayPermissionAndPoint(t *testing.T) {
	contrib := func(m map[string]any, items ...map[string]any) {
		m["contributes"] = map[string]any{"auth_relay": items}
	}
	item := map[string]any{"id": "device", "name": "Device-code sign-in", "hosts": []string{"accounts.example.com"},
		"paths": []string{"/oauth/device/verify"}}
	old, err := Parse(with(t, func(m map[string]any) { m["permissions"] = map[string]any{} }))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(with(t, func(m map[string]any) {
		contrib(m, item)
		m["permissions"] = map[string]any{"auth_relay": true}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.AuthRelays(); len(got) != 1 || got[0].ID != "device" || !slices.Equal(got[0].Hosts, []string{"accounts.example.com"}) {
		t.Fatalf("%+v", got)
	}
	has := func(lines []PermissionLine) bool {
		return slices.ContainsFunc(lines, func(l PermissionLine) bool { return l.Key == "auth_relay" && l.Title != "" && l.Detail != "" })
	}
	if !has(Explain(m)) || has(Explain(old)) || !has(Added(old, m)) || has(Added(m, m)) {
		t.Errorf("explain %+v added %+v", Explain(m), Added(old, m))
	}
	if old.Permissions.Covers(m.Permissions) || !m.Permissions.Covers(old.Permissions) || Digest(old) == Digest(m) {
		t.Error("covers / digest")
	}
	// It starts off at install and when an upgrade adds it, whatever off_by_default says; the user's choice stays after.
	if got := m.StartsOff(nil, nil); !slices.Equal(got, []string{"auth_relay/device"}) {
		t.Errorf("install: %v", got)
	}
	if got := m.StartsOff(old, []string{"ui/page"}); !slices.Equal(got, []string{"auth_relay/device", "ui/page"}) {
		t.Errorf("upgrade adding it: %v", got)
	}
	if got := m.StartsOff(m, nil); len(got) != 0 {
		t.Errorf("upgrade that had it: %v", got)
	}
	withItem := func(edit func(map[string]any)) func(map[string]any) {
		return func(m map[string]any) {
			it := map[string]any{}
			for k, v := range item {
				it[k] = v
			}
			edit(it)
			contrib(m, it)
			m["permissions"] = map[string]any{"auth_relay": true}
		}
	}
	for want, edit := range map[string]func(map[string]any){
		"needs permissions.auth_relay": func(m map[string]any) { contrib(m, item) },
		"hosts":                        withItem(func(it map[string]any) { delete(it, "hosts") }),
		"paths":                        withItem(func(it map[string]any) { it["paths"] = []string{} }),
		"exact lowercase host":         withItem(func(it map[string]any) { it["hosts"] = []string{"*.example.com"} }),
		"no query":                     withItem(func(it map[string]any) { it["paths"] = []string{"/verify?code=1"} }),
	} {
		if _, err := Parse(with(t, edit)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

// A link is named by host and path only, over https, with no user or port.
func TestAuthRelayNames(t *testing.T) {
	a := AuthRelayItem{ID: "device", Hosts: []string{"accounts.example.com"}, Paths: []string{"/oauth/device/verify"}}
	for link, want := range map[string]bool{
		"https://accounts.example.com/oauth/device/verify?user_code=ABCD":   true,
		"https://ACCOUNTS.example.com/oauth/device/verify/?user_code=ABCD":  true,
		" https://accounts.example.com/oauth/device/verify ":                true,
		"http://accounts.example.com/oauth/device/verify?user_code=ABCD":    false,
		"https://accounts.example.com:8443/oauth/device/verify":             false,
		"https://me@accounts.example.com/oauth/device/verify":               false,
		"https://accounts.example.com.evil.test/oauth/device/verify":        false,
		"https://sso.example.com/oauth/device/verify":                       false,
		"https://accounts.example.com/oauth/authorize?redirect_uri=x":       false,
		"https://accounts.example.com/oauth/device/verify/../../authorize":  false,
		"https://accounts.example.com/oauth/device/verify%2F..%2Fauthorize": false,
		"not a link": false,
	} {
		if got := AuthRelayNames(a, link); got != want {
			t.Errorf("%q: %v, want %v", link, got, want)
		}
	}
}
