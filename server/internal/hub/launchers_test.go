package hub

import (
	"testing"
)

func TestLauncherKeySealsAndOpens(t *testing.T) {
	h := &Hub{cfg: &Config{dir: t.TempDir()}}
	sealed, err := h.sealKey("sk-test")
	if err != nil || sealed == "" || sealed == "sk-test" {
		t.Fatalf("sealed %q %v", sealed, err)
	}
	h2 := &Hub{cfg: h.cfg} // a restart reads the same secret.key
	if plain, err := h2.openKey(sealed); err != nil || plain != "sk-test" {
		t.Fatalf("opened %q %v", plain, err)
	}
}

func TestLauncherInputCheck(t *testing.T) {
	ok := launcherInput{Name: "jv_strong", BaseURL: "https://gw.example.com", Model: "m1", Args: []string{"--dangerously-skip-permissions"}}
	if err := ok.check(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []launcherInput{
		{Name: "claude", BaseURL: "https://gw.example.com", Model: "m"},
		{Name: "a b", BaseURL: "https://gw.example.com", Model: "m"},
		{Name: "x", BaseURL: "gw.example.com", Model: "m"},
		{Name: "x", BaseURL: "https://gw.example.com"},
		{Name: "x", BaseURL: "https://gw.example.com", Model: "m", Args: []string{"; rm -rf ~"}},
	} {
		if bad.check() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
