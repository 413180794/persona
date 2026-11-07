package hub

import (
	"github.com/nwverse/persona/server/internal/compat"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompatibleAdminCookiePresenceAndPasswordChange(t *testing.T) {
	names := compat.AdminCookieNames(false)
	request := httptest.NewRequest("GET", "http://example.com/", nil)
	request.AddCookie(&http.Cookie{Name: names[1], Value: "legacy"})
	cookie, err := compatibleAdminCookie(request)
	if err != nil || cookie.Value != "legacy" {
		t.Fatal("legacy cookie was not read")
	}
	request.AddCookie(&http.Cookie{Name: names[0], Value: ""})
	cookie, err = compatibleAdminCookie(request)
	if err != nil || cookie.Value != "" {
		t.Fatal("empty current cookie fell back to legacy credential")
	}
	_, _, mux := adminTestHub(t)
	client := &adminClient{t: t, h: mux, remote: ip4(192, 168, 1, 20) + ":5000"}
	code, _, _ := client.do("POST", "/api/v1/admin/setup", `{"password":"fixture password"}`)
	if code != 200 {
		t.Fatal("setup", code)
	}
	client.cookie.Name = names[1]
	code, output, _ := client.do("POST", "/api/v1/admin/password", `{"current":"fixture password","new":"new fixture password"}`)
	if code != 200 {
		t.Fatalf("legacy cookie password change: %d %v", code, output)
	}
	if code, _, _ := client.do("GET", "/api/v1/admin/overview", ""); code != 200 {
		t.Fatalf("current session was lost: %d", code)
	}
}
