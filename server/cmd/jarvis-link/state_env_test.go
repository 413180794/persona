package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceGeneratorsKeepTheSelectedStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile with $ & % chars")
	bin := filepath.Join(root, "bin", "jarvisd")
	logPath := filepath.Join(root, "node-attach.log")
	for name, contents := range map[string]string{
		"node":      nodeUnitFile(bin, logPath),
		"connector": unitFile(filepath.Join(root, "bin", "jarvis-link"), filepath.Join(root, "link.log")),
	} {
		for _, prefix := range []string{"PERSONA_HOME=", "JARVIS_HOME="} {
			if !strings.Contains(contents, "Environment="+systemdEnvironmentQuote(prefix+root)) {
				t.Errorf("%s service lost %s after the installing shell exits", name, prefix)
			}
		}
	}
	if strings.Contains(systemdEnvironmentQuote("PERSONA_HOME="+root), "$$") {
		t.Fatal("Environment= must not use ExecStart's dollar expansion escape")
	}
	plist := nodePlist(bin, logPath)
	for _, prefix := range []string{"PERSONA_HOME", "JARVIS_HOME"} {
		if !strings.Contains(plist, "<key>"+prefix+"</key><string>") {
			t.Errorf("launchd lost %s after the installing shell exits", prefix)
		}
	}
	if !strings.Contains(plist, "&amp;") || strings.Contains(plist, " with $ & % chars") {
		t.Fatal("state root was not XML-escaped")
	}
}
