//go:build persona_dev_resources && linux

package sourcevalidate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
)

func requireDevelopmentValidation(t *testing.T, factory launch) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	cmd, dispose, err := factory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer dispose()
	if len(cmd.ExtraFiles) == 0 {
		t.Skip("independent kernel resources available; fallback-only fixture is not applicable")
	}
}

func TestDevelopmentValidatorEntryCannotSelfAuthorize(t *testing.T) {
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	self, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	source := []byte("export default 1")
	requireDevelopmentValidation(t, func(ctx context.Context) (*exec.Cmd, func(), error) {
		return sandbox.ValidationProcess(ctx, self, Command, []string{digest(source)}, memoryMB, cpuPercent)
	})
	for _, mode := range []string{"bare-launcher", "bare-child", "sealed-without-limits", "unsealed"} {
		t.Run(mode, func(t *testing.T) {
			cmd, dispose, err := sandbox.ValidationProcess(ctx, self, Command, []string{digest(source)}, memoryMB, cpuPercent)
			if err != nil {
				t.Fatal(err)
			}
			defer dispose()
			if mode == "bare-launcher" {
				cmd.ExtraFiles = nil
			} else {
				cmd.Args = []string{self, Command, "development-validator-v1", digest(source)}
				if mode == "bare-child" {
					cmd.ExtraFiles = nil
				}
				if mode == "unsealed" {
					f, err := os.CreateTemp(t.TempDir(), "unsealed")
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					cmd.ExtraFiles = []*os.File{f}
				}
			}
			cmd.Env = append(cmd.Env, sandbox.DevelopmentResourceEnv+"=1")
			cmd.Stdin = bytes.NewReader(source)
			out, err := cmd.Output()
			if err == nil || len(out) != 0 {
				t.Fatalf("untrusted child reached parser: %v %q", err, out)
			}
		})
	}
	reader := &readCount{}
	if Run([]string{"development-validator-v1", digest(source)}, reader, io.Discard) == 0 || reader.calls != 0 {
		t.Fatal("bare child/environment reached input before policy verification")
	}
}

func TestNativeValidatorBudgetsKillOnlyActualChild(t *testing.T) {
	requireDevelopmentValidation(t, processFixture(t, Command))
	peer := neighbor(t)
	var before, after syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_CPU, &before); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, entered, killed string }{
		{"validator-test-cpu", "native_cpu_entered", "signal: killed"},
		{"validator-test-heap", "native_heap_entered", "terminated=sampled_rss"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			factory := processFixture(t, tc.mode)
			var logs bytes.Buffer
			started := time.Now()
			err := validate(context.Background(), strings.NewReader("x"), digest([]byte("x")), func(ctx context.Context) (*exec.Cmd, func(), error) {
				cmd, dispose, err := factory(ctx)
				if cmd != nil {
					cmd.Stderr = &logs
				}
				return cmd, dispose, err
			})
			if !errors.Is(err, ErrUnavailable) || time.Since(started) >= deadline || !strings.Contains(logs.String(), tc.entered) || !strings.Contains(logs.String(), tc.killed) {
				t.Fatalf("actual native budget not exercised: %v elapsed=%s log=%s", err, time.Since(started), logs.String())
			}
			if strings.Contains(logs.String(), "old_heap_max_mib") {
				t.Fatal("native validator mislabeled as V8")
			}
			ping(t, peer)
			t.Logf("%s elapsed=%s neighbor_responsive=true; sampled metrics are not hard RSS limits\n%s", tc.mode, time.Since(started), logs.String())
		})
	}
	if err := syscall.Getrlimit(syscall.RLIMIT_CPU, &after); err != nil || before != after {
		t.Fatal("manager CPU limit changed", before, after, err)
	}
	if err := fixtureValidate(t, context.Background(), []byte("export default 1")); err != nil {
		t.Fatal("validation did not recover", err)
	}
}
