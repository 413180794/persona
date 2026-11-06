package link

import (
	"strings"
	"testing"
)

// What the hub shows on the machine's page when the link cannot reach the node: how this computer runs it, not a
// script Windows does not have (#1984).
func TestNodeMissingSaysHowThisComputerRunsTheNode(t *testing.T) {
	for goos, want := range map[string]string{
		"windows": "persona-link install --personad",
		"darwin":  "菜单栏 App「Persona Link」",
		"linux":   "persona-link node install",
		"freebsd": "personad node --attach",
	} {
		msg := nodeMissing(goos, "/home/u/.persona/node-attach.sock").Error()
		if !strings.HasPrefix(msg, "本机的常驻 node 没在运行（/home/u/.persona/node-attach.sock 连不上）：") || !strings.Contains(msg, want) {
			t.Errorf("%s: %s", goos, msg)
		}
		if strings.Contains(msg, "node-launchd.sh") {
			t.Errorf("%s: names the old script: %s", goos, msg)
		}
	}
}

// The hub reads the marker as error_code node_service_down (hub/machine_trust.go), also when the node is reached
// over TCP and no socket file name is in the message.
func TestNodeMissingCarriesTheMarker(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux", "freebsd"} {
		if msg := nodeMissing(goos, "127.0.0.1:7001").Error(); !strings.Contains(msg, NodeDownMarker) {
			t.Errorf("%s: %s", goos, msg)
		}
	}
}
