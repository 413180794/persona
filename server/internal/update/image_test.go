package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseImage(t *testing.T) {
	for _, c := range []struct {
		in, registry, repo, tag, name string
		pinned                        bool
	}{
		{"ghcr.io/Owner/Jarvis:latest", "ghcr.io", "owner/jarvis", "latest", "ghcr.io/owner/jarvis", false},
		{"ghcr.io/Owner/Persona:latest", "ghcr.io", "owner/persona", "latest", "ghcr.io/owner/persona", false},
		{"ghcr.io/owner/jarvis", "ghcr.io", "owner/jarvis", "latest", "ghcr.io/owner/jarvis", false},
		{"ghcr.io/owner/jarvis:1.2.0", "ghcr.io", "owner/jarvis", "1.2.0", "ghcr.io/owner/jarvis", true},
		{"ghcr.io/owner/jarvis:edge@sha256:0123", "ghcr.io", "owner/jarvis", "edge", "ghcr.io/owner/jarvis", false},
		{"localhost:5000/owner/jarvis:v1.3.0-rc1", "localhost:5000", "owner/jarvis", "v1.3.0-rc1", "localhost:5000/owner/jarvis", true},
		{"me/jarvis:0.3.0", "registry-1.docker.io", "me/jarvis", "0.3.0", "me/jarvis", true},
		{"postgres", "registry-1.docker.io", "library/postgres", "latest", "postgres", false},
	} {
		im, err := ParseImage(c.in)
		if err != nil || im.Registry != c.registry || im.Repo != c.repo || im.Tag != c.tag || im.Name() != c.name || im.Pinned() != c.pinned {
			t.Errorf("%s: %+v name %s pinned %v, %v", c.in, im, im.Name(), im.Pinned(), err)
		}
	}
	for _, bad := range []string{"", "  ", "ghcr.io/owner/jarvis:", "a b"} {
		if _, err := ParseImage(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		latest, running string
		newer           bool
	}{
		{"0.4.0", "0.3.0", true},
		{"0.10.0", "0.9.0", true},
		{"0.3.0", "0.3.0", false},
		{"0.3.0", "0.4.0", false},
		{"0.4.0", "0.4.0-rc1", true},
		{"0.4.0-rc2", "0.4.0-rc1", true},
		{"0.4.0", "v0.3.0", true},
		{"0.4.0", "v0.3.0-12-gabc1234-202611021500", false}, // a branch build: cannot tell
		{"0.4.0", "dev", false},
		{"edge", "0.3.0", false},
	} {
		if got := NewerVersion(c.latest, c.running); got != c.newer {
			t.Errorf("NewerVersion(%s, %s) = %v", c.latest, c.running, got)
		}
	}
}

// registry is a fake registry with ghcr.io's bearer flow: tags on two pages, a pull token from /token (anonymous
// unless private, then only for the password "secret").
func registry(t *testing.T, private bool) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("scope") != "repository:owner/jarvis:pull" || r.URL.Query().Get("service") != "fake" {
				http.Error(w, "bad scope", http.StatusBadRequest)
				return
			}
			if _, pass, _ := r.BasicAuth(); private && pass != "secret" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `{"token":"pull-token"}`)
		case "/v2/owner/jarvis/tags/list":
			if r.Header.Get("Authorization") != "Bearer pull-token" {
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="fake",scope="repository:owner/jarvis:pull"`, srv.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("last") == "" {
				w.Header().Set("Link", `</v2/owner/jarvis/tags/list?last=latest&n=1000>; rel="next"`)
				fmt.Fprint(w, `{"name":"owner/jarvis","tags":["0.2.0","0.9.0","latest"]}`)
				return
			}
			fmt.Fprint(w, `{"name":"owner/jarvis","tags":["0.10.0","0.11.0-rc1","edge","sha-abc1234","v0.9.5"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestVersion(t *testing.T) {
	ctx := context.Background()
	srv := registry(t, false)
	im, _ := ParseImage(strings.TrimPrefix(srv.URL, "http://") + "/owner/jarvis:latest")
	got, err := LatestVersion(ctx, srv.Client(), im, "")
	if err != nil || got != "0.10.0" {
		t.Fatalf("latest %q, %v (want 0.10.0: both pages, no pre-release, edge or sha tags)", got, err)
	}

	priv := registry(t, true)
	im, _ = ParseImage(strings.TrimPrefix(priv.URL, "http://") + "/owner/jarvis")
	if _, err := LatestVersion(ctx, priv.Client(), im, ""); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a private image without a token: %v", err)
	}
	if _, err := LatestVersion(ctx, priv.Client(), im, "wrong"); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a private image with a wrong token: %v", err)
	}
	if got, err := LatestVersion(ctx, priv.Client(), im, "secret"); err != nil || got != "0.10.0" {
		t.Fatalf("a private image with its token: %q, %v", got, err)
	}

	im, _ = ParseImage(strings.TrimPrefix(srv.URL, "http://") + "/owner/gone")
	if _, err := LatestVersion(ctx, srv.Client(), im, ""); err == nil || !strings.Contains(err.Error(), "没有") {
		t.Fatalf("an image the registry does not have: %v", err)
	}
}
