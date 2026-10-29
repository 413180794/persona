//go:build linux

package sysmon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// ReadSystem reads /etc/os-release, the kernel release, DMI and /proc/cpuinfo.
func ReadSystem(ctx context.Context) proto.System {
	osr, err := os.ReadFile("/etc/os-release")
	if err != nil {
		osr, _ = os.ReadFile("/usr/lib/os-release")
	}
	s := linuxSystem(parseOSRelease(osr))
	s.Arch, s.Cores = runtime.GOARCH, runtime.NumCPU()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		s.Kernel = strings.TrimSpace(string(b))
	}
	vendor, _ := os.ReadFile("/sys/devices/virtual/dmi/id/sys_vendor")
	product, _ := os.ReadFile("/sys/devices/virtual/dmi/id/product_name")
	s.Model = dmiModel(string(vendor), string(product))
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		s.CPU = cpuModel(b)
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		s.Memory = parseMeminfo(b)["MemTotal"]
	}
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		_, _, s.BootTime = parseProcStat(b)
	}
	return s
}

type linuxCollector struct {
	cpu   cpuTimes
	cores []cpuTimes
	net   netCounters
}

// NewCollector starts sampling (nothing runs between samples on Linux).
func NewCollector(ctx context.Context) Collector { return &linuxCollector{} }

func (c *linuxCollector) Close() {}

func (c *linuxCollector) Sample(now time.Time) proto.MetricSample {
	s := proto.MetricSample{T: now.Unix()}
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		all, cores, _ := parseProcStat(b)
		s.CPU = cpuPercent(c.cpu, all)
		if len(cores) == len(c.cores) {
			for i := range cores {
				if v := cpuPercent(c.cores[i], cores[i]); v != nil {
					s.Cores = append(s.Cores, *v)
				}
			}
			if len(s.Cores) != len(cores) {
				s.Cores = nil
			}
		}
		c.cpu, c.cores = all, cores
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		s.Load = parseLoadavg(b)
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		s.MemUsed, s.MemTotal, s.SwapUsed, s.SwapTotal = linuxMemory(parseMeminfo(b))
	}
	if b, err := os.ReadFile("/proc/net/dev"); err == nil {
		cur := netCounters{at: now}
		for name, v := range parseNetDev(b) {
			if physical(name) {
				cur.rx += v[0]
				cur.tx += v[1]
			}
		}
		s.NetRx, s.NetTx = netRates(c.net, cur)
		c.net = cur
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		s.DiskUsed, s.DiskTotal = diskUsage(st.Blocks, st.Bfree, st.Bavail, uint64(st.Bsize))
	}
	return s
}

// physical: the kernel puts every software interface (lo, docker0, veth*, br-*, tun*, tailscale0, wg0, bonds and
// VLANs on top of real ports) under /sys/devices/virtual/net; real NICs, virtio ones in a VM included, sit under
// their bus. Without /sys the name decides.
func physical(name string) bool {
	if target, err := filepath.EvalSymlinks(filepath.Join("/sys/class/net", name)); err == nil {
		return !strings.Contains(target, "/devices/virtual/")
	}
	return !virtualName(name)
}
