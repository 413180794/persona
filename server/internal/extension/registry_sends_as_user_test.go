package extension

import (
	"strings"
	"testing"
)

// connectorManifest is the example manifest as extension id with these connectors.
func connectorManifest(t *testing.T, id string, conns ...map[string]any) *Manifest {
	t.Helper()
	list := []any{}
	for _, c := range conns {
		c["capabilities"] = []any{"status"}
		list = append(list, c)
	}
	return mustParse(t)(with(t, func(m map[string]any) {
		m["id"] = id
		m["contributes"] = map[string]any{"connectors": list}
	}))
}

// Registry.SendsAsUser, the check behind sent.record: an enabled, compatible extension's own connector that declares
// sends_as_user, read from the manifest as installed. A connector's own switch (Settings › Connectors) and the contribution
// switches do not take it away: the send already happened.
func TestRegistrySendsAsUser(t *testing.T) {
	r := NewRegistry(t.TempDir(), nil, nil, 1)
	for _, m := range []*Manifest{
		connectorManifest(t, "ext-chat", map[string]any{"id": "recordchat", "name": "Chat", "sends_as_user": true}, map[string]any{"id": "recordquiet", "name": "Quiet"}),
		connectorManifest(t, "ext-other", map[string]any{"id": "recordother", "name": "Other", "sends_as_user": true}),
	} {
		if err := r.AddBuiltin(m, t.TempDir(), true); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		what, ext, conn string
		want            bool
	}{
		{"its own connector that declares it", "ext-chat", "recordchat", true},
		{"its own connector that does not", "ext-chat", "recordquiet", false},
		{"another extension's connector that declares it", "ext-chat", "recordother", false},
		{"the other extension's own", "ext-other", "recordother", true},
		{"a connector it does not have", "ext-chat", "nobody", false},
		{"no connector named", "ext-chat", "", false},
		{"an extension that is not there", "ext-none", "recordchat", false},
	} {
		if got := r.SendsAsUser(c.ext, c.conn); got != c.want {
			t.Errorf("%s: %v, want %v", c.what, got, c.want)
		}
	}

	set := func(f func(p *Extension)) {
		r.mu.Lock()
		defer r.mu.Unlock()
		f(r.extensions["ext-chat"])
	}
	set(func(p *Extension) { p.Record.Enabled = false })
	if r.SendsAsUser("ext-chat", "recordchat") {
		t.Error("a disabled extension")
	}
	set(func(p *Extension) { p.Record.Enabled = true; p.Problem = "needs a newer hub" })
	if r.SendsAsUser("ext-chat", "recordchat") {
		t.Error("an extension that cannot run on this hub")
	}
	set(func(p *Extension) { p.Problem = "" })
	if !r.SendsAsUser("ext-chat", "recordchat") {
		t.Fatal("back to normal")
	}

	// A connector is switched elsewhere: the contribution switches cannot take it out (Without keeps it), and what the
	// manifest in force says does not matter, the manifest as installed (what the user agreed to) does.
	set(func(p *Extension) {
		p.Record.DisabledContributions = []string{"connectors/recordchat"}
		p.Manifest = p.Full.Without(p.Record.DisabledContributions)
	})
	if !r.SendsAsUser("ext-chat", "recordchat") {
		t.Error("a connector named in DisabledContributions is still the extension's own")
	}
	set(func(p *Extension) { p.Manifest = &Manifest{ID: "ext-chat"} })
	if !r.SendsAsUser("ext-chat", "recordchat") {
		t.Error("the manifest in force has no say, the one installed does")
	}
}

// Installing is refused for a connector id another extension or the hub already has, so the id a sent.record names is
// the one extension's (a second extension cannot declare github, or another's connector).
func TestInstallRefusesAConnectorIdAnotherHas(t *testing.T) {
	r := NewRegistry(t.TempDir(), nil, nil, 1)
	r.Reserve("connectors", "github")
	if err := r.AddBuiltin(connectorManifest(t, "ext-chat", map[string]any{"id": "recordchat", "name": "Chat", "sends_as_user": true}), t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
	conflicts := func(m *Manifest) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.conflicts(m)
	}
	if err := conflicts(connectorManifest(t, "ext-copy", map[string]any{"id": "recordchat", "name": "Copy", "sends_as_user": true})); err == nil ||
		!strings.Contains(err.Error(), "recordchat") || !strings.Contains(err.Error(), "ext-chat") {
		t.Errorf("another extension's connector id: %v", err)
	}
	if err := conflicts(connectorManifest(t, "ext-gh", map[string]any{"id": "github", "name": "GitHub", "sends_as_user": true})); err == nil ||
		!strings.Contains(err.Error(), "github") {
		t.Errorf("a connector the hub has: %v", err)
	}
	if err := conflicts(connectorManifest(t, "ext-new", map[string]any{"id": "recordnew", "name": "New"})); err != nil {
		t.Errorf("a connector id nobody has: %v", err)
	}
}
