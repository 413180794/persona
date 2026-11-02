package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/update"
)

// The app reads what the hub is from /api/v1/health (and the hello): its channel, build number and protocol, to
// show the version and say when one of them is too old (docs/hub-update.md「兼容」).
func TestHealthReportsTheBuild(t *testing.T) {
	h := newTestHub(t)
	h.build = update.Make("v0.2.0-3-gabc-202611021530", "release", "202611021530", "https://example.com/dl")
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Channel     string `json:"channel"`
		Build       int64  `json:"build"`
		Protocol    int    `json:"protocol"`
		MinAppBuild *int   `json:"min_app_build"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Channel != "release" || got.Build != 202611021530 || got.Protocol != proto.Protocol || got.MinAppBuild == nil {
		t.Fatalf("%+v", got)
	}
}
