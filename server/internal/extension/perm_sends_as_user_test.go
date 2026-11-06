package extension

import (
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// withConnectors is the example manifest with these connectors (each a map, with at least an id and a name).
func withConnectors(t *testing.T, conns ...map[string]any) *Manifest {
	t.Helper()
	list := []any{}
	for _, c := range conns {
		if _, ok := c["capabilities"]; !ok {
			c["capabilities"] = []any{"status"}
		}
		list = append(list, c)
	}
	return mustParse(t)(with(t, func(m map[string]any) { m["contributes"] = map[string]any{"connectors": list} }))
}

func sendsLine(lines []PermissionLine) (PermissionLine, bool) {
	i := slices.IndexFunc(lines, func(l PermissionLine) bool { return l.Key == "sends_as_user" })
	if i < 0 {
		return PermissionLine{}, false
	}
	return lines[i], true
}

// A connector that sends in the user's name on its own (sends_as_user) is shown on install with its name, in every
// language, and is not shown for one that does not.
func TestSendsAsUserIsShownOnInstall(t *testing.T) {
	plain := withConnectors(t, map[string]any{"id": "examplechat", "name": "Example Chat"})
	sends := withConnectors(t, map[string]any{"id": "examplechat", "name": "Example Chat", "sends_as_user": true},
		map[string]any{"id": "examplemail", "name": "Example Mail"})
	if got := sends.Connectors(); !got[0].SendsAsUser || got[1].SendsAsUser {
		t.Fatalf("not read: %+v", got)
	}
	if _, ok := sendsLine(Explain(plain)); ok {
		t.Errorf("a connector that does not declare it: %+v", Explain(plain))
	}
	for _, l := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		line, ok := sendsLine(ExplainIn(l, sends))
		if !ok || line.Title == "" || !strings.Contains(line.Detail, "Example Chat") || strings.Contains(line.Detail, "Example Mail") ||
			strings.Contains(line.Title+line.Detail, "extension-permissions.") || strings.Contains(line.Detail, "{names}") {
			t.Errorf("%s: %+v", l, line)
		}
	}
	if line, _ := sendsLine(Explain(sends)); line.Title != i18n.T(i18n.Default, "extension-permissions.sends_as_user_title") {
		t.Errorf("title: %+v", line)
	}
}

// Adding sends_as_user on an upgrade is more than the user agreed to: it is listed as added, it needs the user again
// (MorePermissions) and the digest they agree to changes. Keeping it, or taking it away, is not.
func TestSendsAsUserAddedOnUpgradeAsksAgain(t *testing.T) {
	conn := func(sends bool, id, name string) map[string]any {
		c := map[string]any{"id": id, "name": name}
		if sends {
			c["sends_as_user"] = true
		}
		return c
	}
	old := withConnectors(t, conn(false, "examplechat", "Example Chat"), conn(false, "examplemail", "Example Mail"))
	m := withConnectors(t, conn(true, "examplechat", "Example Chat"), conn(false, "examplemail", "Example Mail"))
	both := withConnectors(t, conn(true, "examplechat", "Example Chat"), conn(true, "examplemail", "Example Mail"))
	installed := func(x *Manifest) *Extension {
		return &Extension{Record: Record{Granted: x.Permissions, GrantedDigest: Digest(x)}, Manifest: x, Full: x}
	}

	line, ok := sendsLine(Added(old, m))
	if !ok || !strings.Contains(line.Detail, "Example Chat") || line.Title != i18n.T(i18n.Default, "extension-permissions.sends_as_user_more_title") {
		t.Fatalf("added: %+v", Added(old, m))
	}
	if !MorePermissions(installed(old), m) || Digest(old) == Digest(m) {
		t.Error("an upgrade that adds sends_as_user is agreed to again")
	}
	if Added(m, m) != nil || MorePermissions(installed(m), m) {
		t.Error("keeping it asks nothing")
	}
	if _, ok := sendsLine(Added(m, old)); ok || MorePermissions(installed(m), old) {
		t.Error("taking it away asks nothing")
	}
	// A second connector that sends: only the new one is added.
	line, ok = sendsLine(Added(m, both))
	if !ok || !strings.Contains(line.Detail, "Example Mail") || strings.Contains(line.Detail, "Example Chat") || !MorePermissions(installed(m), both) {
		t.Errorf("a second connector: %+v", Added(m, both))
	}
	for _, l := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		if line, ok := sendsLine(AddedIn(l, old, m)); !ok || strings.Contains(line.Title+line.Detail, "extension-permissions.") {
			t.Errorf("%s: %+v", l, line)
		}
	}
	// The same connectors in another order are the same agreement.
	swapped := withConnectors(t, conn(true, "examplemail", "Example Mail"), conn(true, "examplechat", "Example Chat"))
	if Digest(both) != Digest(swapped) {
		t.Error("the digest depends on the order of the connectors")
	}
}

// What users agreed to before the field existed holds: a manifest whose connectors do not send as the user digests
// as it did (the canonical JSON of the digest has no sends_as_user key).
func TestSendsAsUserLeavesOldDigestsAlone(t *testing.T) {
	m := withConnectors(t, map[string]any{"id": "examplechat", "name": "Example Chat"})
	if got := sendsAsUserIDs(m); len(got) != 0 {
		t.Fatalf("ids %v", got)
	}
	plain := mustParse(t)([]byte(exampleManifest))
	if Digest(plain) == Digest(withConnectors(t, map[string]any{"id": "examplenotes", "name": "Example Notes", "sends_as_user": true})) {
		t.Error("the digest does not see it")
	}
	// false is the same as absent.
	off := withConnectors(t, map[string]any{"id": "examplenotes", "name": "Example Notes", "sends_as_user": false, "capabilities": []any{"status", "tools"},
		"hosts": []any{"notes.example.com"}, "poll_seconds": 600})
	if Digest(off) != Digest(plain) {
		t.Error("sends_as_user false changed the digest")
	}
}

// The field is a boolean.
func TestSendsAsUserMustBeABoolean(t *testing.T) {
	_, err := Parse(with(t, func(m map[string]any) {
		m["contributes"] = map[string]any{"connectors": []any{map[string]any{"id": "examplechat", "name": "Example Chat",
			"capabilities": []any{"status"}, "sends_as_user": "yes"}}}
	}))
	if err == nil {
		t.Fatal("a string was taken")
	}
}
