package extension

import "testing"

// The official market is its repository however the owner wrote it; other repositories and sites are not.
func TestOfficialMarketIs(t *testing.T) {
	o := OfficialMarket{Spec: "github:nwverse/persona-extensions"}
	for _, spec := range []string{"github:nwverse/persona-extensions", "github:NWverse/Persona-Extensions@main",
		"https://github.com/nwverse/persona-extensions", "https://github.com/nwverse/persona-extensions/tree/master",
		"git:https://github.com/nwverse/persona-extensions.git", "git@github.com:nwverse/persona-extensions.git#main"} {
		if !o.Is(spec) {
			t.Errorf("%s is the official market", spec)
		}
	}
	for _, spec := range []string{"github:nwverse/persona-removed", "github:someone/persona-extensions",
		"https://example.com/persona-extensions.json", "git:https://git.example.com/nwverse/persona-extensions.git", "nonsense"} {
		if o.Is(spec) {
			t.Errorf("%s is not the official market", spec)
		}
	}
	if (OfficialMarket{}).Is("github:nwverse/persona-extensions") {
		t.Error("no official market matches nothing")
	}
	if _, err := ParseSigningKey(Official.SigningKey); err != nil {
		t.Errorf("the official key: %v", err)
	}
	if spec, err := ParseMarket(Official.Spec); err != nil || spec.String() != Official.Spec {
		t.Errorf("the official spec: %v %v", spec, err)
	}
}
