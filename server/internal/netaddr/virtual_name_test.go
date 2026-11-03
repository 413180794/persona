package netaddr

import "testing"

func TestVirtualBoxNamesRemainCaseInsensitive(t *testing.T) {
	for _, name := range []string{"vboxnet", "vboxnet0", "VBOXNET1", "vBoXnEt12"} {
		if !virtualName(name) {
			t.Errorf("missed virtual interface %q", name)
		}
	}
	for _, name := range []string{"", "vbox0", "my-vboxnet0", "eth0", "en0"} {
		if virtualName(name) {
			t.Errorf("misclassified physical/unknown interface %q", name)
		}
	}
}
