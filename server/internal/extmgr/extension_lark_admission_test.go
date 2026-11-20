//go:build linux || darwin

package extmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/extension"
)

func TestLarkAdmissionCapabilityCannotRetireAnotherRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gate")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	m := &Manager{opts: Options{Admission: g}}
	installed := &extension.Extension{Record: extension.Record{ID: "lark-context", Source: extension.SourceInstalled},
		Manifest: &extension.Manifest{Runtime: &extension.Runtime{}}}
	capability := larkAdmissionRPC(installed, m)
	if capability == nil {
		t.Fatal("local installed runtime did not receive the capability")
	}
	other := *installed
	other.Record.ID = "unrelated"
	remote := *installed
	remote.Record.Remote = &extension.Remote{}
	for _, p := range []*extension.Extension{&other, &remote} {
		if _, err := larkAdmissionRPC(p, m).call("new-host-run", "maintenance.lark.begin", json.RawMessage(`{}`)); err == nil {
			t.Fatal("another extension received Lark maintenance access")
		}
	}
	receipt, err := capability.call("first-host-run", "maintenance.lark.begin", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := json.Marshal(receipt.(map[string]string)["lease"])
	if err != nil {
		t.Fatal(err)
	}
	const attempt = "extension-attempt"
	if _, err := g.Control("close", attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := capability.call("new-host-run", "maintenance.lark.end", lease); err == nil {
		t.Fatal("another runtime could retire the admitted operation")
	}
	if _, err := capability.call("new-host-run", "maintenance.lark.begin", json.RawMessage(`{}`)); err == nil {
		t.Fatal("replacement admitted work across a closed fence")
	}
	if _, err := g.Control("open", attempt); err == nil {
		t.Fatal("the original lease vanished")
	}
	if _, err := capability.call("first-host-run", "maintenance.lark.end", lease); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Control("open", attempt); err != nil {
		t.Fatal(err)
	}
}
