package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
)

// serveAuthRelayExtensionForTest answers auth_relay.complete by the link's case= (done, manual, not_handled, bogus,
// boom), writing each call's params as a line of JSON to RELAY_OUT.
func serveAuthRelayExtensionForTest() {
	out := os.Getenv("RELAY_OUT")
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-relay", Version: "1.0.0",
		Namespaces: map[string]pserve.Handler{
			"auth_relay": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != "complete" {
					return nil, pserve.UnknownMethod(method)
				}
				if f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
					_, _ = f.Write(append(append([]byte{}, params...), '\n'))
					_ = f.Close()
				}
				var p wire.AuthRelayCompleteParams
				_ = json.Unmarshal(params, &p)
				u, _ := url.Parse(p.URL)
				switch u.Query().Get("case") {
				case "done":
					return wire.AuthRelayResult{Status: "done", App: "Example\nCLI " + strings.Repeat("x", 100), Reason: "new_permissions"}, nil
				case "manual":
					return wire.AuthRelayResult{Status: "manual", App: "Example CLI", Reason: "new_permissions"}, nil
				case "not_handled":
					return wire.AuthRelayResult{Status: "manual", Reason: "not_handled"}, nil
				case "bogus":
					return wire.AuthRelayResult{Status: "maybe", Reason: "who knows"}, nil
				}
				return nil, errors.New("the browser fell over at " + p.URL)
			}}})
}

func authRelayPackage(t *testing.T, out string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "relay"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=relay exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-relay", "name": "Example Relay", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/relay"}, "env": {"RELAY_OUT": %q}},
		"activation_events": ["on_startup"],
		"contributes": {"auth_relay": [{"id": "device", "name": "Device-code sign-in", "hosts": ["accounts.example.com"], "paths": ["/oauth/device/verify"]}]},
		"permissions": {"auth_relay": true}}`, runtime.GOOS+"-"+runtime.GOARCH, out)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

func TestAuthRelay(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	ctx := context.Background()
	link := func(c string) string {
		return "https://accounts.example.com/oauth/device/verify?user_code=ABCD-EFGH&case=" + c
	}
	if h.AuthRelays(link("done")) {
		t.Fatal("no extension: none takes it")
	}
	if _, ok := h.CompleteAuth(ctx, link("done")); ok {
		t.Fatal("no extension: not taken")
	}

	out := filepath.Join(t.TempDir(), "got.jsonl")
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+authRelayPackage(t, out)+`"}`)
	if !strings.Contains(fmt.Sprint(pv["permissions"]), "auth_relay") {
		t.Fatalf("the permission is shown before installing: %v", pv["permissions"])
	}
	call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	up := func() bool {
		hp := h.extensions.get("example-relay")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	}
	waitLong(t, "the extension up", up)
	if h.AuthRelays(link("done")) {
		t.Fatal("an auth_relay contribution starts off, whatever the manifest says")
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/extensions/example-relay/-/contributions/auth_relay/device", master, `{"enabled": true}`); code != 200 {
		t.Fatalf("switch on: %d", code)
	}
	waitLong(t, "the contribution on, the extension up again", func() bool { return h.AuthRelays(link("done")) && up() })

	if !h.AuthRelays(link("done")) || h.AuthRelays("https://sso.example.com/oauth/device/verify?case=done") ||
		h.AuthRelays("http://accounts.example.com/oauth/device/verify?case=done") {
		t.Fatal("only the hosts and paths it names, over https")
	}
	r, ok := h.CompleteAuth(ctx, link("done"))
	if !ok || r.Status != wire.AuthRelayStatusDone || r.Reason != "" || strings.Contains(r.App, "\n") || len([]rune(r.App)) > 80 ||
		!strings.HasPrefix(r.App, "Example CLI x") {
		t.Fatalf("done: one short line for the app, no reason: %+v %v", r, ok)
	}
	raw, _ := os.ReadFile(out)
	var got wire.AuthRelayCompleteParams
	_ = json.Unmarshal([]byte(strings.SplitN(string(raw), "\n", 2)[0]), &got)
	if got.Contribution != "device" || got.URL != link("done") {
		t.Fatalf("the contribution and the link as given: %+v", got)
	}
	if r, ok := h.CompleteAuth(ctx, link("manual")); !ok || r.Status != wire.AuthRelayStatusManual || r.Reason != wire.AuthRelayReasonNewPermissions || r.App != "Example CLI" {
		t.Fatalf("manual: %+v %v", r, ok)
	}
	if r, ok := h.CompleteAuth(ctx, link("not_handled")); ok {
		t.Fatalf("not one it completes: not taken: %+v", r)
	}
	if r, ok := h.CompleteAuth(ctx, link("bogus")); !ok || r.Status != wire.AuthRelayStatusFailed || r.Reason != "" {
		t.Fatalf("a status it does not know is a failure: %+v %v", r, ok)
	}
	if r, ok := h.CompleteAuth(ctx, link("boom")); !ok || r.Status != wire.AuthRelayStatusFailed || r.Reason != wire.AuthRelayReasonUnavailable {
		t.Fatalf("an error: failed, unavailable: %+v %v", r, ok)
	}

	// Its contribution off: not asked.
	if code, _ := call(t, mux, "PUT", "/api/v1/extensions/example-relay/-/contributions/auth_relay/device", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("switch off: %d", code)
	}
	waitLong(t, "the contribution off", func() bool { return !h.AuthRelays(link("done")) })
}
