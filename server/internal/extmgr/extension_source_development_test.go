//go:build persona_dev_resources && linux

package extmgr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/denohost"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/extension/sourcevalidate"
)

// Exercise the actual manager's sourceSpec/confineSource, not an alternative
// wrapper. No hub, node, database or live configuration is involved.
func TestSourceDevelopmentManagerActivation(t *testing.T) {
	c := denohost.Config{Path: os.Getenv("TEST_DENO_PATH"), SHA256: os.Getenv("TEST_DENO_SHA256"), Version: denohost.Version}
	if c.Path == "" {
		t.Skip("pinned engine variables required")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	if dev, err := sandbox.SourceDevelopmentResources(self); err != nil {
		t.Fatal(err)
	} else if !dev {
		t.Skip("independent kernel resources available; fallback-only fixture is not applicable")
	}
	controls := []int{syscall.RLIMIT_CPU, syscall.RLIMIT_NOFILE, syscall.RLIMIT_FSIZE, syscall.RLIMIT_CORE}
	limitsBefore := make([]syscall.Rlimit, len(controls))
	for i, control := range controls {
		if err := syscall.Getrlimit(control, &limitsBefore[i]); err != nil {
			t.Fatal(err)
		}
	}
	fdsBefore, _ := os.ReadDir("/proc/self/fd")
	root, err := os.MkdirTemp("", "sm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	bundle := filepath.Join(root, "main.mjs")
	source := `let init;export default {id:"example",version:"1.0.0",init(v){init=v},namespaces:{probe:async(m)=>{
if(m==="inspect")return {config:init.config,secrets:init.secrets};
if(m==="deny"){const out={};for(const [key,f] of Object.entries({env:()=>Deno.env.get("HOME"),read:()=>Deno.readTextFileSync("/etc/passwd"),run:()=>new Deno.Command("/bin/true").outputSync(),ffi:()=>Deno.dlopen("/lib/libc.so.6",{})})){try{f();out[key]="allowed"}catch{out[key]="denied"}}return out}
return {};
}}};`
	if err := os.WriteFile(bundle, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(source))
	p := &extension.Extension{Dir: root, Record: extension.Record{ID: "example", Source: extension.SourceInstalled}, Manifest: &extension.Manifest{Runtime: &extension.Runtime{Mode: extension.RuntimeDeno, Source: &extension.SourceRuntime{Bundle: "main.mjs", SHA256: hex.EncodeToString(hash[:])}}}}
	var mu sync.Mutex
	warnings := 0
	logf := func(f string, a ...any) {
		line := fmt.Sprintf(f, a...)
		mu.Lock()
		if strings.HasPrefix(line, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK extension=") {
			warnings++
		}
		mu.Unlock()
		t.Log(line)
	}
	ps := &extensions{root: filepath.Join(root, "manager"), reg: &extension.Registry{}, logf: logf, h: &Manager{cfg: &Config{ExtensionRuntime: &c}}}
	spec := host.Spec{ID: "example", Dir: root, CleanEnv: true, RPC: true, Startup: true, Logf: logf, Init: func(context.Context) (extension.InitializeParams, error) {
		return extension.InitializeParams{Protocol: extension.Protocol, Config: json.RawMessage(`{"owner":"example"}`), Secrets: map[string]string{"key": "synthetic-example-secret"}}, nil
	}}
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	original := p.Manifest.Runtime.Source
	for _, forbidden := range []string{`import("node:fs")`, `export default import.meta`, `export {x} from "https://example.com/module"`} {
		if err := os.WriteFile(filepath.Join(root, "forbidden.mjs"), []byte(forbidden), 0600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(forbidden))
		p.Manifest.Runtime.Source = &extension.SourceRuntime{Bundle: "forbidden.mjs", SHA256: hex.EncodeToString(hash[:])}
		if err := ps.sourceSpec(context.Background(), p, &spec); !errors.Is(err, sourcevalidate.ErrInvalid) {
			t.Fatal("manager allowed module loading before source activation", err)
		}
	}
	p.Manifest.Runtime.Source = original
	if err := ps.sourceSpec(context.Background(), p, &spec); err != nil {
		t.Fatal(err)
	}
	// An extension-controlled child environment must not opt the manager in.
	spec.Env = append(spec.Env, sandbox.DevelopmentResourceEnv+"=1")
	for _, value := range []string{"", "0", "true", "yes", "01", "1 ", " 1"} {
		t.Setenv(sandbox.DevelopmentResourceEnv, value)
		if err := ps.confineSource(context.Background(), p, &spec); err == nil {
			t.Fatalf("manager accepted non-explicit opt-in %q or child environment", value)
		}
	}
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	if err := ps.confineSource(context.Background(), p, &spec); err != nil {
		t.Fatal(err)
	}
	defer ps.sandboxes.proxyIfStarted().Close()
	proc := host.Start(context.Background(), spec)
	defer proc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := proc.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	var out json.RawMessage
	if err := proc.Call(ctx, "probe.inspect", nil, &out); err != nil || !strings.Contains(string(out), "synthetic-example-secret") {
		t.Fatalf("manager init: %s %v", out, err)
	}
	if err := proc.Call(ctx, "probe.deny", nil, &out); err != nil || strings.Contains(string(out), "allowed") {
		t.Fatalf("manager native gates: %s %v", out, err)
	}
	mu.Lock()
	count := warnings
	mu.Unlock()
	if count != 1 {
		t.Fatal("manager warning not emitted on activation", count)
	}
	proc.Stop()
	// Restart gets the same activation budget, not what remains after calls and stop.
	restartCtx, restartCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer restartCancel()
	restarted := host.Start(context.Background(), spec)
	defer restarted.Stop()
	if err := restarted.Wait(restartCtx); err != nil {
		t.Fatal("manager restart failed", err)
	}
	mu.Lock()
	count = warnings
	mu.Unlock()
	if count != 2 {
		t.Fatal("manager warning not repeated on restart", count)
	}
	restarted.Stop()
	ps.sandboxes.proxyIfStarted().Close()
	for i, control := range controls {
		var actual syscall.Rlimit
		if err := syscall.Getrlimit(control, &actual); err != nil || actual != limitsBefore[i] {
			t.Fatal("source activation changed the manager's own limits", control, limitsBefore[i], actual, err)
		}
	}
	fdsAfter, _ := os.ReadDir("/proc/self/fd")
	if len(fdsAfter) > len(fdsBefore)+2 {
		t.Fatal("source restart/stop leaked descriptors", len(fdsBefore), len(fdsAfter))
	}
	b, err := os.ReadFile(bundle)
	if err != nil || sha256.Sum256(b) != hash {
		t.Fatal("source activation changed the package's signed bytes", err)
	}
	t.Log("real manager confinement accepted exact opt-in and warned on restart; child-env opt-in and native escapes refused; manager limits, bundle digest and descriptors preserved")
}
