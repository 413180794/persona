package node

import (
	"os/exec"
	"slices"
	"testing"
)

func TestModelProbeVerifiesForwardingAfterEnvironmentUnset(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	definitions := `clearing() { unset ANTHROPIC_MODEL; command claude "$@" }
not_forwarding() { command claude }
fixed() { command claude --model fixed-model "$@" }
`
	for _, tc := range []struct {
		name  string
		valid bool
	}{{"clearing", true}, {"not_forwarding", false}, {"fixed", false}} {
		out, err := exec.Command(zsh, "-fc", definitions+"\n"+probeScript(tc.name, "--model", "requested-model")).Output()
		if err != nil {
			t.Fatal(err)
		}
		probe, ok := parseProbe(string(out))
		if !ok {
			t.Fatal("probe did not run")
		}
		if err := forwardedModel(probe, "requested-model"); (err == nil) != tc.valid {
			t.Fatalf("%s: %+v %v", tc.name, probe.Args, err)
		}
	}
}

func TestManagedModelOverrideRemovesDuplicateFlags(t *testing.T) {
	got := withoutModelArgs([]string{"--model=old", "--verbose", "--model", "old", "--permission-mode", "acceptEdits"})
	if !slices.Equal(got, []string{"--verbose", "--permission-mode", "acceptEdits"}) {
		t.Fatalf("%v", got)
	}
}
