//go:build persona_dev_resources && linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/osproc"
	"golang.org/x/sys/unix"
)

// Only ValidationProcess in an explicitly enabled manager creates this sealed
// descriptor. It carries no source, configuration, secret or guest authority.
// FD 3 crosses only the trusted launchers, not the Deno guest. A bare child CLI
// or a child environment opt-in cannot manufacture a successful pre-parse gate.
const validationControls = "persona-development-native-validator-v1 cpu_seconds=2 rss_sample_mib=128 wall_ms=5000 nofile=128 fsize=8388608 core=0\n"
const validationSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

func developmentValidationProcess(ctx context.Context, self, command string, args []string) (*exec.Cmd, func(), error) {
	if os.Getenv(DevelopmentResourceEnv) != "1" {
		return nil, nil, errors.New("explicit manager development opt-in required")
	}
	self, err := filepath.EvalSymlinks(self)
	if err != nil {
		return nil, nil, err
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, nil, err
	}
	fd, err := unix.MemfdCreate("persona-validator-policy", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, nil, err
	}
	policy := os.NewFile(uintptr(fd), "validator-policy")
	fail := func(err error) (*exec.Cmd, func(), error) { _ = policy.Close(); return nil, nil, err }
	if _, err := policy.WriteString(validationControls); err != nil {
		return fail(err)
	}
	if _, err := unix.FcntlInt(policy.Fd(), unix.F_ADD_SEALS, validationSeals); err != nil {
		return fail(err)
	}
	if _, err := policy.Seek(0, 0); err != nil {
		return fail(err)
	}
	argv := []string{ValidationRunCommand, developmentValidationPolicy, self, command, bwrap}
	cmd := exec.CommandContext(ctx, self, append(argv, args...)...)
	cmd.Env, cmd.Dir = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "LANG=C"}, "/"
	cmd.ExtraFiles = []*os.File{policy}
	fmt.Fprintf(os.Stderr, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK validator=go_native cpu_lifetime_seconds=2 cooperative_memory_mib=96 sampled_rss_kill_mib=128 sample_ms=25 wall_ms=5000 nofile=128 fsize_bytes=8388608 core=0 hard_total_rss=false cpu_quota=false pids_hard_limit=false: %s\n", i18n.T(i18n.Default, "extension-source.development_validator_resources"))
	return cmd, func() { _ = policy.Close() }, nil
}

func verifyDevelopmentPolicyDescriptor() error { return verifyDevelopmentPolicyFD(3) }

func verifyDevelopmentPolicyFD(fd int) error {
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil || seals&validationSeals != validationSeals {
		return errors.New("sealed development validator policy required")
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Size != int64(len(validationControls)) {
		return errors.New("invalid development validator policy")
	}
	b := make([]byte, len(validationControls))
	if n, err := unix.Pread(fd, b, 0); err != nil || n != len(b) || string(b) != validationControls {
		return errors.New("invalid development validator policy")
	}
	return nil
}

func verifyDevelopmentValidationEntry() error {
	if err := verifyDevelopmentPolicyDescriptor(); err != nil {
		return err
	}
	for _, l := range []struct {
		kind  int
		value uint64
	}{
		{syscall.RLIMIT_CPU, 2}, {syscall.RLIMIT_CORE, 0},
		{syscall.RLIMIT_NOFILE, 128}, {syscall.RLIMIT_FSIZE, 8 << 20},
	} {
		var actual syscall.Rlimit
		if err := syscall.Getrlimit(l.kind, &actual); err != nil || actual.Cur != l.value || actual.Max != l.value {
			return errors.New("native validator child limit not enforced")
		}
	}
	return nil
}

func runDevelopmentValidation(args []string) int {
	if len(args) < 2 {
		return 2
	}
	var policy *os.File
	if args[0] == developmentValidationSupervisor {
		// bwrap consumes FD 3 into an exact read-only mount. Re-seal that fixed
		// trusted policy in the supervisor, never from guest input or environment.
		mountedFD, err := unix.Open("/run/persona/validator-policy", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return 2
		}
		mounted := os.NewFile(uintptr(mountedFD), "validator-mounted-policy")
		defer mounted.Close()
		st, err := mounted.Stat()
		var fs unix.Statfs_t
		if err != nil || !st.Mode().IsRegular() || st.Size() != int64(len(validationControls)) || unix.Fstatfs(int(mounted.Fd()), &fs) != nil || fs.Flags&unix.ST_RDONLY == 0 {
			return 2
		}
		b, err := io.ReadAll(io.LimitReader(mounted, int64(len(validationControls))+1))
		if err != nil || string(b) != validationControls {
			return 2
		}
		fd, err := unix.MemfdCreate("persona-validator-child-policy", unix.MFD_ALLOW_SEALING)
		if err != nil {
			return 2
		}
		policy = os.NewFile(uintptr(fd), "validator-child-policy")
		defer policy.Close()
		if n, err := unix.Write(fd, b); err != nil || n != len(b) {
			return 2
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, validationSeals); err != nil {
			return 2
		}
		if verifyDevelopmentPolicyFD(fd) != nil {
			return 2
		}
	}
	// Never overwrite FD 3 in a running Go process: netpoll may own it after
	// reading the mounted policy. ExtraFiles maps it only in the new exec child.
	if policy == nil {
		if err := verifyDevelopmentPolicyDescriptor(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if args[0] == "development-validator-exec-v1" {
		return runDevelopmentExec(append([]string{"2", "--"}, args[1:]...), os.Stderr)
	}
	if args[0] == developmentValidationSupervisor {
		// Inside the same narrow namespace, supervise a separately reaped native
		// child. No V8 heap claim: Go-native runtime controls apply in Run.
		return runDevelopmentNativeChild(append([]string{"2", "128", "5000", "--", SelfPath, args[1], developmentValidationPolicy}, args[2:]...), SelfPath, os.Stderr, policy)
	}
	if len(args) < 4 || args[0] != developmentValidationPolicy {
		return 2
	}
	ns, err := ValidationNamespace(args[1], ValidationRunCommand,
		append([]string{developmentValidationSupervisor, args[2]}, args[4:]...), []Mount{{DataFD: 3, Target: "/run/persona/validator-policy"}})
	if err != nil {
		return 1
	}
	if err := osproc.Exec(args[3], append([]string{args[3]}, ns...), []string{"PATH=/usr/bin:/bin"}); err != nil {
		return 1
	}
	return 0
}
