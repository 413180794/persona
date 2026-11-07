package devices

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestDeployErrorsKeepCanonicalTextAndCause(t *testing.T) {
	remote := &rpc.Error{Code: "node_update_busy", Message: i18n.T(i18n.Default, "devices-deploy.node_busy")}
	err := explain(i18n.Err("devices-deploy.install_update_failed", i18n.Args{"err": remote.Error()}), remote)
	var coded *rpc.Error
	if !errors.As(err, &coded) || coded.Code != remote.Code || !errors.Is(err, remote) {
		t.Fatalf("lost node retry code: %v", err)
	}
	want := i18n.T(i18n.Default, "devices-deploy.install_update_failed", i18n.Args{"err": remote.Error()})
	if err.Error() != want {
		t.Fatalf("changed stored/log text: %q", err.Error())
	}
	for _, l := range []i18n.Lang{i18n.En, i18n.Ja} {
		got := i18n.DeploymentText(l, i18n.Localize(err, l))
		want := i18n.T(l, "devices-deploy.install_update_failed", i18n.Args{"err": i18n.T(l, "devices-deploy.node_busy")})
		if got != want {
			t.Fatalf("nested node error in %s: %q want %q", l, got, want)
		}
	}
	if _, err := nodeBinary("", "", "", ""); err == nil || err.Error() != "还不知道这台电脑的系统，连上以后再试" {
		t.Fatalf("legacy error changed: %v", err)
	}
}

func TestSavedDeploymentErrorRelocalizesAfterRestart(t *testing.T) {
	d, m, _, _ := linkedDeployFixture(t, kindLink)
	original := i18n.T(i18n.Default, "devices-deploy.hub_changed")
	record := nodeDeployRecord{Target: "old-build", State: "failed", Error: original}
	if err := m.saveDeployRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	// Round-trip through the Store and a new machine, as reconnect after a manager/hub restart does.
	restored := newMachine(d, m.cfg)
	restored.nodeUpdateConnected(context.Background(), nil, m.info)
	raw, err := json.Marshal(restored.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if restored.snapshot().DeployError != original {
		t.Fatalf("record lost canonical error: %s", raw)
	}
	for _, l := range []i18n.Lang{i18n.En, i18n.Ja, i18n.ZhHans} {
		got := i18n.DeploymentText(l, restored.snapshot().DeployError)
		if want := i18n.T(l, "devices-deploy.hub_changed"); got != want {
			t.Fatalf("%s: got %q want %q", l, got, want)
		}
	}
	var saved nodeDeployRecord
	if _, err := d.opts.Store.Setting(context.Background(), m.deployRecordKey(), &saved); err != nil || saved.Error != original {
		t.Fatalf("language switch rewrote record: %+v %v", saved, err)
	}
}
