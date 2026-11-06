package sourcevalidate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		if strings.HasPrefix(os.Args[1], "validator-test-") && os.Args[1] != "validator-test-neighbor" {
			if len(os.Args) < 3 || sandbox.VerifyValidationEntry(memoryMB, cpuPercent, os.Args[2]) != nil {
				os.Exit(1)
			}
			childControls()
		}
		switch os.Args[1] {
		case sandbox.ValidationRunCommand:
			os.Exit(sandbox.RunValidation(os.Args[2:]))
		case "validator-test-allocation":
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var result boundedResult
			code := run(os.Args[3:], os.Stdin, &result)
			runtime.ReadMemStats(&after)
			if code != 0 || string(result.bytes[:result.n]) != "ok\n" {
				os.Exit(1)
			}
			_, _ = fmt.Fprintf(os.Stdout, "%d\n", after.TotalAlloc-before.TotalAlloc)
			os.Exit(0)
		case sandbox.RunCommand:
			os.Exit(sandbox.Run(os.Args[2:], os.Stderr))
		case Command:
			os.Exit(Run(os.Args[2:], os.Stdin, os.Stdout))
		case "validator-test-cpu":
			fmt.Fprintln(os.Stderr, "native_cpu_entered")
			for {
			}
		case "validator-test-heap":
			fmt.Fprintln(os.Stderr, "native_heap_entered")
			var allocations [][]byte
			for {
				b := make([]byte, 16<<20)
				for i := range b {
					b[i] = 1
				}
				allocations = append(allocations, b)
				runtime.KeepAlive(allocations)
			}
		case "validator-test-hang":
			for {
				time.Sleep(time.Hour)
			}
		case "validator-test-output":
			for {
				_, _ = os.Stdout.Write(make([]byte, 4096))
			}
		case "validator-test-neighbor":
			p := rpc.New("neighbor", os.Stdout)
			p.Limits = rpc.Limits{FrameBytes: 1024, ConcurrentRequests: 2, PendingCalls: 2}
			p.OnRequest = func(_ context.Context, _ string, params json.RawMessage) (any, error) { return params, nil }
			_ = p.Serve(context.Background(), os.Stdin)
			os.Exit(0)
		case "validator-test-isolation":
			ns, err := os.Readlink("/proc/self/ns/net")
			if err != nil || ns == os.Args[4] || os.Getenv("PERSONA_VALIDATOR_PRIVATE_TEST") != "" {
				os.Exit(1)
			}
			if _, err := os.ReadFile(os.Args[3]); err == nil {
				os.Exit(1)
			}
			if os.WriteFile("/tmp/validator-write", []byte("x"), 0o600) == nil {
				os.Exit(1)
			}
			c, err := net.DialTimeout("tcp", "192.0.2.1:80", 50*time.Millisecond)
			if c != nil {
				_ = c.Close()
			}
			if err == nil {
				os.Exit(1)
			}
			_, _ = io.WriteString(os.Stdout, "ok\n")
			os.Exit(0)
		case "validator-test-marker":
			_, _ = io.WriteString(os.Stdout, "parser-called\n")
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type readCount struct{ calls int }

func (r *readCount) Read([]byte) (int, error) { r.calls++; return 0, io.EOF }

func TestUnavailableAndInvalidNeverReadSource(t *testing.T) {
	bare := &readCount{}
	if code := Run([]string{"persona-ext-invalid.scope", digest([]byte("x"))}, bare, io.Discard); code == 0 || bare.calls != 0 {
		t.Fatal("bare validator reached input/parser without kernel limits", code, bare.calls)
	}
	for _, d := range []string{digest([]byte("x")), "invalid"} {
		reader := &readCount{}
		launched := false
		err := validate(context.Background(), reader, d, func(context.Context) (*exec.Cmd, func(), error) {
			launched = true
			return nil, nil, errors.New("unavailable")
		})
		if err == nil || reader.calls != 0 || (d == "invalid" && launched) {
			t.Fatal(err, reader.calls, launched)
		}
	}
	self, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// This is the actual native verifier before exec, not a mocked parser.
	cmd := exec.CommandContext(ctx, self, sandbox.RunCommand, "--resource-check", "0", "50", "persona-ext-invalid.scope", "--", self, "validator-test-marker")
	out, err := cmd.Output()
	if err == nil || bytes.Contains(out, []byte("parser-called")) {
		t.Fatalf("invalid resources reached parser: %v %q", err, out)
	}
}

func TestProductionKernelAcceptance(t *testing.T) {
	t.Setenv(sandbox.DevelopmentResourceEnv, "")
	b := []byte("export default 1")
	err := Validate(context.Background(), bytes.NewReader(b), digest(b))
	if errors.Is(err, ErrUnavailable) {
		t.Skip("UNAVAILABLE: independent kernel source-validator acceptance; gate #5975 remains open")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{[]byte("import('./x.mjs')"), []byte("export default (")} {
		if err := Validate(context.Background(), bytes.NewReader(b), digest(b)); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}

// Test helpers use the exact production API with the tagged manager opt-in,
// sealed policy and both entry gates. No independently unbounded parser fixture.
func processFixture(t *testing.T, helper string) launch {
	t.Helper()
	if runtime.GOOS != "linux" || os.Getenv(sandbox.DevelopmentResourceEnv) != "1" {
		t.Skip("explicit tagged Linux manager development-resource opt-in required")
	}
	self, _ := os.Executable()
	if helperPath := os.Getenv("PERSONA_TEST_VALIDATOR_HELPER"); helperPath != "" {
		// Parent race instrumentation is independent of the actual limited,
		// trusted non-race child; TSAN startup is not parser acceptance.
		self = helperPath
	}
	return func(ctx context.Context) (*exec.Cmd, func(), error) {
		cmd, dispose, err := sandbox.ValidationProcess(ctx, self, helper, nil, memoryMB, cpuPercent)
		if cmd != nil {
			cmd.Stderr = os.Stderr
		}
		return cmd, dispose, err
	}
}
func fixtureValidate(t *testing.T, ctx context.Context, b []byte) error {
	t.Helper()
	factory := processFixture(t, Command)
	return validate(ctx, bytes.NewReader(b), digest(b), func(ctx context.Context) (*exec.Cmd, func(), error) {
		cmd, done, err := factory(ctx)
		if err == nil {
			cmd.Args = append(cmd.Args, digest(b))
		}
		return cmd, done, err
	})
}

func TestProcessModulePolicyAndNoExecution(t *testing.T) {
	valid := []string{`export default {text:"import from",imported:true}`, "// import('./x')\nexport default 'import and from'", "export default `text from a service`", `const pattern=/import|from|"/; export default pattern`, `const from=1;export default from`, `export default (()=>{while(true){}})()`}
	for _, source := range valid {
		if err := fixtureValidate(t, context.Background(), []byte(source)); err != nil {
			t.Fatalf("trusted fixture rejected: %v", err)
		}
	}
	invalid := []string{`import/*comment*/("https://example.com/code")`, `export {x} from/*comment*/"./x.mjs"`, `import("node:fs")`, "export default `${import('./x.mjs')}`", `const pattern=/"/; import("./x.mjs")`, `export default import.meta`, `let x="./x.mjs"; export default ()=>import(x)`, `export default (`}
	for _, source := range invalid {
		if err := fixtureValidate(t, context.Background(), []byte(source)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("module policy result: %v", err)
		}
	}
	factory := processFixture(t, Command)
	b := []byte("export default 1")
	err := validate(context.Background(), bytes.NewReader(b), digest(b), func(ctx context.Context) (*exec.Cmd, func(), error) {
		cmd, done, err := factory(ctx)
		if err == nil {
			cmd.Args = append(cmd.Args, strings.Repeat("0", 64))
		}
		return cmd, done, err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed stream digest accepted: %v", err)
	}
}

func neighbor(t *testing.T) *rpc.Peer {
	t.Helper()
	self, _ := os.Executable()
	cmd := exec.Command(self, "validator-test-neighbor")
	cmd.Env, cmd.Dir = []string{"PATH=/usr/bin:/bin"}, "/"
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	peer := rpc.New("parent", stdin)
	peer.Limits = rpc.Limits{FrameBytes: 1024, ConcurrentRequests: 2, PendingCalls: 2}
	done := make(chan struct{})
	go func() { _ = peer.Serve(context.Background(), stdout); close(done) }()
	t.Cleanup(func() { peer.Close(io.EOF); _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait(); <-done })
	return peer
}
func ping(t *testing.T, p *rpc.Peer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out string
	if err := p.Call(ctx, "echo", "responsive", &out); err != nil || out != "responsive" {
		t.Fatalf("neighbor affected: %v %q", err, out)
	}
}

func TestAdversarialParserOnlyChildDies(t *testing.T) {
	processFixture(t, Command)
	peer := neighbor(t)
	for _, tc := range []struct {
		name string
		b    []byte
		good bool
	}{
		{"amplification-256KiB", bytes.Repeat([]byte("a;"), 131072), true},
		{"amplification-16MiB", bytes.Repeat([]byte("a;"), Most/2), false},
		{"deep", []byte(strings.Repeat("(", 100000) + "0" + strings.Repeat(")", 100000)), false},
		{"malformed", []byte("export default ("), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			done := make(chan error, 1)
			go func() { done <- fixtureValidate(t, context.Background(), tc.b) }()
			var err error
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
		wait:
			for {
				select {
				case err = <-done:
					break wait
				case <-tick.C:
					ping(t, peer)
				}
			}
			runtime.ReadMemStats(&after)
			if tc.good && err != nil {
				t.Fatal(err)
			}
			if !tc.good && err == nil {
				t.Fatal("adversarial artifact accepted")
			}
			// Unlike the original 52.3x parser allocation, parent allocation
			// is independent of AST shape. This is NOT a peak RSS measurement.
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
				t.Fatalf("parent allocated %d bytes", allocated)
			}
			t.Logf("verdict=%v parent_allocated_bytes=%d (not peak RSS)", err, after.TotalAlloc-before.TotalAlloc)
			ping(t, peer)
		})
	}
}

func TestCancellationQueueOutputAndCleanup(t *testing.T) {
	processFixture(t, Command)
	fdsBefore, _ := os.ReadDir("/proc/self/fd")
	goroutinesBefore := runtime.NumGoroutine()
	for i := 0; i < 12; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		factory := processFixture(t, "validator-test-hang")
		var process *os.Process
		err := validate(ctx, strings.NewReader("x"), digest([]byte("x")), func(ctx context.Context) (*exec.Cmd, func(), error) {
			cmd, dispose, err := factory(ctx)
			if err == nil {
				// Save the wrapper after Start through disposal.
				return cmd, func() { process = cmd.Process; dispose() }, nil
			}
			return cmd, dispose, err
		})
		cancel()
		if !errors.Is(err, ErrStopped) {
			t.Fatal(err)
		}
		if process != nil && osproc.Alive(process.Pid) {
			t.Fatal("validator wrapper survived cancellation")
		}
		if len(admitted) != 0 || len(running) != 0 {
			t.Fatal("validator queue leaked")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := validate(ctx, strings.NewReader("x"), digest([]byte("x")), processFixture(t, "validator-test-output")); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
	fdsAfter, _ := os.ReadDir("/proc/self/fd")
	if len(fdsAfter) > len(fdsBefore)+1 || runtime.NumGoroutine() > goroutinesBefore+1 {
		t.Fatalf("leak: fd %d -> %d goroutines %d -> %d", len(fdsBefore), len(fdsAfter), goroutinesBefore, runtime.NumGoroutine())
	}
	for i := 0; i < cap(admitted); i++ {
		admitted <- struct{}{}
	}
	if err := validate(context.Background(), strings.NewReader("x"), digest([]byte("x")), nil); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	for i := 0; i < cap(admitted); i++ {
		<-admitted
	}
	if err := fixtureValidate(t, context.Background(), []byte("export default 1")); err != nil {
		t.Fatal(err)
	}
}

func TestParentAndChildAllocationAreSeparate(t *testing.T) {
	factory := processFixture(t, "validator-test-allocation")
	b := bytes.Repeat([]byte("a;"), 131072)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	cmd, dispose, err := factory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer dispose()
	cmd.Args = append(cmd.Args, digest(b))
	cmd.Stdin = bytes.NewReader(b)
	var result boundedResult
	cmd.Stdout, cmd.Stderr = &result, os.Stderr
	cmd.SysProcAttr = osproc.GroupAttr()
	cmd.Cancel = func() error { return osproc.StopGroup(cmd.Process.Pid, true) }
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	_ = osproc.StopGroup(cmd.Process.Pid, true)
	runtime.ReadMemStats(&after)
	child, err := strconv.ParseUint(strings.TrimSpace(string(result.bytes[:result.n])), 10, 64)
	parent := after.TotalAlloc - before.TotalAlloc
	if err != nil || result.overflow || child < 8<<20 || parent > 2<<20 {
		t.Fatalf("allocation boundary parent=%d child=%d parse=%v", parent, child, err)
	}
	t.Logf("parent_total_alloc_delta=%d child_total_alloc_delta=%d; neither is peak heap/RSS or cgroup equivalence", parent, child)
}

func TestBoundedResult(t *testing.T) {
	var result boundedResult
	for i := 0; i < 1000; i++ {
		_, _ = result.Write([]byte("private-source-text-or-path"))
	}
	if !result.overflow || result.n != len(result.bytes) {
		t.Fatal(fmt.Sprint(result))
	}
}
