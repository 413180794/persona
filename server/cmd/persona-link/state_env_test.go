package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceGeneratorsKeepTheSelectedStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile with $ & % chars")
	bin := filepath.Join(root, "bin", "personad")
	logPath := filepath.Join(root, "node-attach.log")
	for name, contents := range map[string]string{
		"node":      nodeUnitFile(bin, logPath),
		"connector": unitFile(filepath.Join(root, "bin", "persona-link"), filepath.Join(root, "link.log")),
	} {
		want := "Environment=" + systemdEnvironmentQuote("PERSONA_HOME="+root)
		if strings.Count(contents, want) != 1 {
			t.Errorf("%s must export the selected state root exactly once", name)
		}
	}
	if strings.Contains(systemdEnvironmentQuote("PERSONA_HOME="+root), "$$") {
		t.Fatal("Environment= must not use ExecStart's dollar expansion escape")
	}
	plist := nodePlist(bin, logPath)
	if strings.Count(plist, "<key>PERSONA_HOME</key><string>") != 1 {
		t.Error("launchd must export the selected state root exactly once")
	}
	if !strings.Contains(plist, "&amp;") || strings.Contains(plist, " with $ & % chars") {
		t.Fatal("state root was not XML-escaped")
	}
}
