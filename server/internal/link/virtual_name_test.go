package link

import "testing"

func TestVirtualBoxTunnelNamesRemainCaseInsensitive(t *testing.T) {
	for _, name := range []string{"vboxnet", "vboxnet0", "VBOXNET1", "vBoXnEt12"} {
		if !tunnelOrBridge(name) {
			t.Errorf("missed tunnel/bridge interface %q", name)
		}
	}
	for _, name := range []string{"", "vbox0", "my-vboxnet0", "eth0", "en0"} {
		if tunnelOrBridge(name) {
			t.Errorf("misclassified physical/unknown interface %q", name)
		}
	}
}
