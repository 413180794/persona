//go:build persona_dev_resources && linux

package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// This re-exec helper changes ONLY its own limits immediately before replacing
// itself with the guest. The manager and the supervising proxy retain their limits.
func runDevelopmentExec(args []string, stderr io.Writer) int {
	if len(args) < 3 || args[1] != "--" {
		return 2
	}
	cpu, err := strconv.Atoi(args[0])
	if err != nil || cpu < 1 || cpu > 30 {
		return 2
	}
	for _, l := range []struct {
		kind  int
		value uint64
	}{
		{syscall.RLIMIT_CPU, uint64(cpu)}, {syscall.RLIMIT_CORE, 0},
		{syscall.RLIMIT_NOFILE, 128}, {syscall.RLIMIT_FSIZE, 8 << 20},
	} {
		if err := syscall.Setrlimit(l.kind, &syscall.Rlimit{Cur: l.value, Max: l.value}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		var actual syscall.Rlimit
		if err := syscall.Getrlimit(l.kind, &actual); err != nil || actual.Cur != l.value || actual.Max != l.value {
			return 1
		}
		fmt.Fprintf(stderr, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK rlimit=%d soft=%d hard=%d\n", l.kind, actual.Cur, actual.Max)
	}
	// No RLIMIT_AS: the pinned V8 sandbox reserves large virtual address ranges.
	// No RLIMIT_RSS: Linux does not enforce it. Sampled RSS is supervised separately.
	if err := syscall.Exec(args[2], args[2:], os.Environ()); err != nil {
		fmt.Fprintln(stderr, err)
		return 127
	}
	return 0
}

func runDevelopmentChild(args []string, self string, stderr io.Writer) int {
	return superviseDevelopmentChild(args, self, stderr, nil)
}

func runDevelopmentNativeChild(args []string, self string, stderr io.Writer, policy *os.File) int {
	return superviseDevelopmentChild(args, self, stderr, policy)
}

func superviseDevelopmentChild(args []string, self string, stderr io.Writer, policy *os.File) int {
	native := policy != nil
	if len(args) < 5 || args[3] != "--" {
		return 2
	}
	cpu, e1 := strconv.Atoi(args[0])
	rss, e2 := strconv.Atoi(args[1])
	wall, e3 := strconv.ParseInt(args[2], 10, 64)
	// Check raw milliseconds before multiplying: oversized integers can wrap
	// time.Duration back into the accepted lifetime range.
	if e1 != nil || e2 != nil || e3 != nil || wall < time.Second.Milliseconds() || wall > (5*time.Minute).Milliseconds() {
		return 2
	}
	d := DevelopmentLimits{CPUSeconds: cpu, RSSMB: rss, Wall: time.Duration(wall) * time.Millisecond}
	if !d.valid() {
		return 2
	}
	profile := "engine=deno old_heap_max_mib=64"
	if native {
		profile = "engine=go_native cooperative_memory_mib=96 max_stack_bytes=2097152 max_threads=16 gomaxprocs=2 pids_hard_limit=false"
	}
	fmt.Fprintf(stderr, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK %s cpu_lifetime_seconds=%d sampled_rss_kill_mib=%d sample_ms=25 wall_ms=%d nofile=128 fsize_bytes=8388608 core=0 hard_total_rss=false cpu_quota=false\n", profile, cpu, rss, wall)
	cmd := exec.Command(self, append([]string{RunCommand, "--development-exec", args[0], "--"}, args[4:]...)...)
	if native {
		// Pass only the sealed trusted policy across both re-execs.
		cmd.ExtraFiles = []*os.File{policy}
		cmd.Args = append([]string{self, ValidationRunCommand, "development-validator-exec-v1"}, args[4:]...)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(stderr, err)
		return 127
	}
	done := make(chan error, 1)
	var peakRSS, peakVM int64
	defer func() {
		fmt.Fprintf(stderr, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK observed_peak_rss_kib=%d observed_peak_vm_kib=%d\n", peakRSS, peakVM)
	}()
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(d.Wall)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigs)
	kill := func(reason string) int {
		fmt.Fprintln(stderr, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK terminated="+reason)
		_ = cmd.Process.Kill()
		<-done
		return 137
	}
	exit := func(err error) int {
		if err == nil {
			return 0
		}
		if e, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(stderr, "DEVELOPMENT_ONLY_RESOURCE_FALLBACK child_exit=%s user_cpu=%s system_cpu=%s\n", e.ProcessState.String(), e.UserTime(), e.SystemTime())
			if st, ok := e.Sys().(syscall.WaitStatus); ok && st.Signaled() {
				return 128 + int(st.Signal())
			}
			return e.ExitCode()
		}
		return 1
	}
	for {
		select {
		case err := <-done:
			return exit(err)
		case <-timer.C:
			return kill("wall_clock")
		case s := <-sigs:
			_ = cmd.Process.Signal(s)
		case <-tick.C:
			b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", cmd.Process.Pid))
			if err != nil {
				// /proc can disappear just before Wait publishes a successful exit.
				// Give that already-exiting child a bounded reaping window rather
				// than reporting a normal completion as watchdog termination.
				if os.IsNotExist(err) {
					reap := time.NewTimer(100 * time.Millisecond)
					select {
					case err := <-done:
						reap.Stop()
						return exit(err)
					case <-reap.C:
					}
				}
				return kill("rss_probe_unavailable")
			}
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "VmSize:") {
					f := strings.Fields(line)
					if len(f) > 1 {
						kb, _ := strconv.ParseInt(f[1], 10, 64)
						peakVM = max(peakVM, kb)
					}
				}
				if strings.HasPrefix(line, "VmRSS:") {
					f := strings.Fields(line)
					if len(f) < 2 {
						return kill("rss_probe_invalid")
					}
					kb, err := strconv.ParseInt(f[1], 10, 64)
					if err != nil {
						return kill("rss_probe_invalid")
					}
					peakRSS = max(peakRSS, kb)
					if kb > int64(rss)*1024 {
						return kill("sampled_rss")
					}
				}
			}
		}
	}
}
