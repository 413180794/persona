package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type failingPrefsStore struct{ Store }

func (failingPrefsStore) SetDevicePrefs(context.Context, string, string, json.RawMessage) error {
	return errors.New("storage unavailable")
}

func TestDevicePrefsStorageFailure(t *testing.T) {
	h := newTestHub(t)
	if err := h.push.setPrefs("device", defaultPrefs()); err != nil {
		t.Fatal(err)
	}
	h.push.store = failingPrefsStore{}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	r, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/devices/device/prefs", strings.NewReader(`{"input":false,"done":"off"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || !reflect.DeepEqual(h.push.prefs("device"), defaultPrefs()) {
		t.Fatalf("failed write = %d, prefs = %+v", resp.StatusCode, h.push.prefs("device"))
	}
}
