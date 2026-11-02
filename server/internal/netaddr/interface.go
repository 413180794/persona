package netaddr

import (
	"path/filepath"
	"strings"
)

func virtualInterface(name string, resolve func(string) (string, error)) bool {
	if target, err := resolve(filepath.Join("/sys/class/net", name)); err == nil {
		return strings.HasPrefix(target, "/sys/devices/virtual/")
	}
	return virtualName(name)
}
