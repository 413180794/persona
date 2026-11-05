package extmgr

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failingForgetStore struct {
	*keepStore
	deleteErr, putErr error
}

func (s *failingForgetStore) DeleteConnectorSecret(ctx context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.keepStore.DeleteConnectorSecret(ctx, id)
}

func (s *failingForgetStore) PutSetting(ctx context.Context, key string, v any) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.keepStore.PutSetting(ctx, key, v)
}

func forgetFixture(t *testing.T, st *failingForgetStore) (*Manager, *strings.Builder) {
	t.Helper()
	ctx := context.Background()
	if err := st.keepStore.PutConnectorSecret(ctx, "example", "sealed"); err != nil {
		t.Fatal(err)
	}
	cs := connectorSettings{Switch: map[string]bool{"example": true, "other": true}, Inbox: map[string]bool{"example": false}}
	if err := st.keepStore.PutSetting(ctx, connectorSettingsKey, cs); err != nil {
		t.Fatal(err)
	}
	h := newTestManager(t)
	h.store = st
	var logs strings.Builder
	logTo(h, &logs)
	return h, &logs
}

func storedSettings(t *testing.T, st *keepStore) connectorSettings {
	t.Helper()
	var cs connectorSettings
	if _, err := st.Setting(context.Background(), connectorSettingsKey, &cs); err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestForgetConnectorDropsSecretAndSettings(t *testing.T) {
	st := &failingForgetStore{keepStore: newKeepStore()}
	h, logs := forgetFixture(t, st)
	h.forgetConnector(context.Background(), "example")
	if _, ok, _ := st.ConnectorSecret(context.Background(), "example"); ok {
		t.Fatal("the removed connector's sign-in is still stored")
	}
	cs := storedSettings(t, st.keepStore)
	if _, ok := cs.Switch["example"]; ok || !cs.Switch["other"] {
		t.Fatalf("switches after forget: %v", cs.Switch)
	}
	if _, ok := cs.Inbox["example"]; ok {
		t.Fatalf("inbox feeds after forget: %v", cs.Inbox)
	}
	if logs.Len() != 0 {
		t.Fatalf("nothing failed, but the hub logged:\n%s", logs)
	}
}

func TestForgetConnectorLogsWhatItCouldNotDelete(t *testing.T) {
	st := &failingForgetStore{keepStore: newKeepStore(), deleteErr: errors.New("database down"), putErr: errors.New("timeout")}
	h, logs := forgetFixture(t, st)
	h.forgetConnector(context.Background(), "example")
	got := logs.String()
	for _, want := range []string{"connector example: removed, but its sign-in is still stored: database down",
		"connector example: removed, but its settings are still stored: timeout"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sealed") {
		t.Errorf("the log carries the sealed secret:\n%s", got)
	}
}

func TestForgetConnectorClearsSettingsWhenTheSecretStays(t *testing.T) {
	st := &failingForgetStore{keepStore: newKeepStore(), deleteErr: errors.New("database down")}
	h, _ := forgetFixture(t, st)
	h.forgetConnector(context.Background(), "example")
	if _, ok := storedSettings(t, st.keepStore).Switch["example"]; ok {
		t.Fatal("a failed secret delete kept the connector's settings too")
	}
}
