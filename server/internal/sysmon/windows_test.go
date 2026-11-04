//go:build windows

package sysmon

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// On the Windows machine running the tests: every field Windows has, from the second sample on.
func TestWindowsSampleHere(t *testing.T) {
	c := NewCollector(context.Background())
	defer c.Close()
	now := time.Now()
	first := c.Sample(now)
	if first.CPU != nil || first.NetRx != nil {
		t.Fatalf("the first sample has rates: %+v", first)
	}
	time.Sleep(1100 * time.Millisecond)
	s := c.Sample(now.Add(1100 * time.Millisecond))
	if s.CPU == nil || *s.CPU < 0 || *s.CPU > 100 {
		t.Fatalf("cpu %v", s.CPU)
	}
	if runtime.NumCPU() <= 64 && len(s.Cores) != runtime.NumCPU() {
		t.Fatalf("%d cores for %d CPUs", len(s.Cores), runtime.NumCPU())
	}
	if s.Load != nil {
		t.Fatal("Windows has no load average")
	}
	if s.MemUsed <= 0 || s.MemUsed > s.MemTotal || s.SwapUsed > s.SwapTotal || s.DiskUsed <= 0 || s.DiskUsed > s.DiskTotal {
		t.Fatalf("sample %+v", s)
	}
	if len(physicalNICs()) > 0 && (s.NetRx == nil || s.NetTx == nil) {
		t.Fatalf("no network rates with NICs %v", physicalNICs())
	}
	for _, d := range diskDrives() {
		if len(d) != 3 || !strings.HasSuffix(d, `:\`) {
			t.Fatalf("drive %q", d)
		}
	}
	sys := ReadSystem(context.Background())
	if !strings.HasPrefix(sys.Name, "Windows") || sys.Distro != "windows" || sys.Build == "" || !strings.HasPrefix(sys.Kernel, "NT ") ||
		sys.Memory <= 0 || sys.BootTime <= 0 || sys.BootTime > time.Now().Unix() {
		t.Fatalf("system %+v", sys)
	}
}
