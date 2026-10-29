package sysmon

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestProcStatCPU(t *testing.T) {
	all0, cores0, boot := parseProcStat(fixture(t, "proc_stat"))
	all1, cores1, _ := parseProcStat(fixture(t, "proc_stat_later"))
	if boot != 1790788452 || len(cores0) != 2 || len(cores1) != 2 {
		t.Fatalf("boot %d, cores %d %d", boot, len(cores0), len(cores1))
	}
	// 2000 jiffies passed, 1200 of them busy (iowait counts as idle).
	if v := cpuPercent(all0, all1); v == nil || *v != 60 {
		t.Fatalf("cpu = %v", v)
	}
	if v := cpuPercent(cores0[0], cores1[0]); v == nil || *v != 66.7 {
		t.Fatalf("cpu0 = %v", v)
	}
	if v := cpuPercent(cpuTimes{}, all1); v != nil {
		t.Fatalf("first reading has a rate: %v", *v)
	}
	if v := cpuPercent(all1, all0); v != nil {
		t.Fatalf("counter went back but got %v", *v)
	}
}

func TestMeminfo(t *testing.T) {
	used, total, swapUsed, swapTotal := linuxMemory(parseMeminfo(fixture(t, "meminfo")))
	if total != 16303428*1024 || used != (16303428-9861756)*1024 {
		t.Fatalf("mem %d / %d", used, total)
	}
	if swapTotal != 4194300*1024 || swapUsed != (4194300-4093948)*1024 {
		t.Fatalf("swap %d / %d", swapUsed, swapTotal)
	}
	// No MemAvailable (old kernels): free + buffers + cache.
	used, _, _, _ = linuxMemory(map[string]int64{"MemTotal": 100, "MemFree": 10, "Buffers": 5, "Cached": 25})
	if used != 60 {
		t.Fatalf("old kernel used = %d", used)
	}
}

func TestNetDevAndVirtualInterfaces(t *testing.T) {
	dev := parseNetDev(fixture(t, "net_dev"))
	if len(dev) != 5 || dev["eth0"] != [2]uint64{5000000000, 1000000000} || dev["docker0"][1] != 654321 {
		t.Fatalf("net dev = %v", dev)
	}
	var kept []string
	for name := range dev {
		if !virtualName(name) {
			kept = append(kept, name)
		}
	}
	if len(kept) != 1 || kept[0] != "eth0" {
		t.Fatalf("physical by name = %v", kept)
	}
	for _, name := range []string{"en0", "enp3s0", "eno1", "wlan0", "wlp2s0", "ens5"} {
		if virtualName(name) {
			t.Errorf("%s taken for virtual", name)
		}
	}
	for _, name := range []string{"utun3", "wg0", "tun0", "br-1a2b", "virbr0", "awdl0", "llw0", "bridge0", "zt5u4y"} {
		if !virtualName(name) {
			t.Errorf("%s taken for physical", name)
		}
	}
}

func TestNetRates(t *testing.T) {
	t0 := time.Unix(1000, 0)
	prev := netCounters{rx: 1000, tx: 500, at: t0}
	rx, tx := netRates(prev, netCounters{rx: 21000, tx: 1500, at: t0.Add(10 * time.Second)})
	if rx == nil || *rx != 2000 || *tx != 100 {
		t.Fatalf("rates %v %v", rx, tx)
	}
	if rx, _ := netRates(netCounters{}, prev); rx != nil {
		t.Fatal("first reading has a rate")
	}
	// An interface went away: the sum dropped. No negative or wrapped spike.
	if rx, _ := netRates(prev, netCounters{rx: 10, tx: 600, at: t0.Add(10 * time.Second)}); rx != nil {
		t.Fatal("rate across a counter reset")
	}
}

func TestOSRelease(t *testing.T) {
	s := linuxSystem(parseOSRelease(fixture(t, "os-release-ubuntu")))
	if s.Distro != "ubuntu" || s.Like != "debian" || s.Name != "Ubuntu 24.04.1 LTS" || s.Version != "24.04" {
		t.Fatalf("ubuntu = %+v", s)
	}
	s = linuxSystem(parseOSRelease(fixture(t, "os-release-debian")))
	if s.Distro != "debian" || s.Name != "Debian GNU/Linux 12 (bookworm)" || s.Version != "12" {
		t.Fatalf("debian = %+v", s)
	}
	if s := linuxSystem(nil); s.Distro != "linux" || s.Name != "Linux" {
		t.Fatalf("no os-release = %+v", s)
	}
}

func TestHardwareNames(t *testing.T) {
	if m := cpuModel(fixture(t, "cpuinfo")); m != "AMD Ryzen 9 7950X 16-Core Processor" {
		t.Fatalf("x86 cpu = %q", m)
	}
	if m := cpuModel(fixture(t, "cpuinfo_arm")); m != "BCM2835" {
		t.Fatalf("arm cpu = %q", m)
	}
	for _, c := range [][3]string{
		{"Dell Inc.\n", "OptiPlex 7090\n", "Dell Inc. OptiPlex 7090"},
		{"LENOVO", "ThinkCentre LENOVO M75q", "ThinkCentre LENOVO M75q"},
		{"System manufacturer", "System Product Name", ""},
		{"QEMU", "Standard PC (Q35 + ICH9, 2009)", "QEMU Standard PC (Q35 + ICH9, 2009)"},
		{"", "", ""},
	} {
		if got := dmiModel(c[0], c[1]); got != c[2] {
			t.Errorf("dmi %q %q = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	name, version, build := parseSwVers(fixture(t, "sw_vers"))
	if name != "macOS" || version != "26.6.2" || build != "25G83" {
		t.Fatalf("sw_vers = %q %q %q", name, version, build)
	}
	if m := profilerModel(fixture(t, "system_profiler")); m != "Mac Studio" {
		t.Fatalf("model = %q", m)
	}
}

func TestVMStat(t *testing.T) {
	var page uint64
	var rows []vmStat
	sc := bufio.NewScanner(strings.NewReader(string(fixture(t, "vm_stat"))))
	for sc.Scan() {
		if p, ok := parseVMStatHeader(sc.Text()); ok {
			page = p
		} else if v, ok := parseVMStatRow(sc.Text()); ok {
			rows = append(rows, v)
		}
	}
	if page != 16384 || len(rows) != 2 {
		t.Fatalf("page %d, %d rows (the column header is not a row)", page, len(rows))
	}
	v := rows[1]
	if v.anonymous != 1003369 || v.purgeable != 4150 || v.wired != 700618 || v.compressor != 1921486 || v.free != 8142 {
		t.Fatalf("row = %+v", v)
	}
	if used := darwinMemoryUsed(v, page); used != (1003369-4150+700618+1921486)*16384 {
		t.Fatalf("used = %d", used)
	}
	if n, ok := abbreviated("12456M"); !ok || n != 12456<<20 {
		t.Fatalf("abbreviated = %d", n)
	}
}

func TestIostat(t *testing.T) {
	var busy, load []float64
	for _, line := range strings.Split(string(fixture(t, "iostat")), "\n") {
		if b, l, ok := parseIostatRow(line); ok {
			busy, load = append(busy, b), append(load, l)
		}
	}
	if len(busy) != 2 || busy[0] != 66 || busy[1] != 37 || load[0] != 18.76 {
		t.Fatalf("iostat busy %v load %v", busy, load)
	}
}

func TestDiskUsage(t *testing.T) {
	// df: 1K-blocks 971350180, available 614658756, used 333036572 → capacity 36%.
	used, total := diskUsage(971350180, 971350180-333036572, 614658756, 1024)
	if used != 333036572*1024 || total != (333036572+614658756)*1024 {
		t.Fatalf("disk %d / %d", used, total)
	}
}

func TestSampleHere(t *testing.T) {
	c := NewCollector(context.Background())
	defer c.Close()
	now := time.Now()
	_ = c.Sample(now)
	s := c.Sample(now.Add(time.Second))
	if s.MemTotal == 0 || s.DiskTotal == 0 {
		t.Fatalf("sample on this machine = %+v", s)
	}
	if sys := ReadSystem(context.Background()); sys.Cores == 0 || sys.Kernel == "" || sys.Name == "" {
		t.Fatalf("system = %+v", sys)
	}
}
