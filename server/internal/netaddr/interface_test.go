package netaddr

import (
	"errors"
	"testing"
)

func TestVirtualInterfaceSysfsWins(t *testing.T) {
	tests := []struct {
		name, target string
		err          error
		want         bool
	}{
		{"eth0", "/sys/devices/virtual/net/eth0", nil, true},
		{"tun0", "/sys/devices/pci0000:00/net/tun0", nil, false},
		{"eth0", "/sys/devices/pci0000:00/virtio0/net/eth0", nil, false},
		{"wg0", "", errors.New("missing sysfs"), true},
		{"en0", "", errors.New("missing sysfs"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := virtualInterface(tt.name, func(path string) (string, error) {
				if path != "/sys/class/net/"+tt.name {
					t.Fatal("unexpected sysfs path")
				}
				return tt.target, tt.err
			})
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
