package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakePE is the smallest image setSubsystem reads: an MZ header pointing at a PE header with a PE32+ optional header
// whose Subsystem is subsystem.
func fakePE(subsystem uint16) []byte {
	const pe = 0x80
	b := make([]byte, pe+4+20+240)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3c:], pe)
	copy(b[pe:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[pe+4+16:], 240) // SizeOfOptionalHeader
	opt := pe + 4 + 20
	binary.LittleEndian.PutUint16(b[opt:], 0x20b)
	binary.LittleEndian.PutUint16(b[opt+68:], subsystem)
	return b
}

func subsystemOf(t *testing.T, b []byte) uint16 {
	t.Helper()
	opt := int(binary.LittleEndian.Uint32(b[0x3c:])) + 4 + 20
	return binary.LittleEndian.Uint16(b[opt+68:])
}

func TestSetSubsystem(t *testing.T) {
	b := fakePE(peSubsystemConsole)
	if err := setSubsystem(b, peSubsystemConsole, peSubsystemGUI); err != nil {
		t.Fatal(err)
	}
	if got := subsystemOf(t, b); got != peSubsystemGUI {
		t.Fatalf("subsystem %d", got)
	}
	// Already GUI, a script, a cut-off file: refused, nothing changed.
	if err := setSubsystem(b, peSubsystemConsole, peSubsystemGUI); err == nil || !strings.Contains(err.Error(), "subsystem is 2") {
		t.Fatalf("GUI image: %v", err)
	}
	for name, bad := range map[string][]byte{
		"script":    []byte("#!/bin/sh\nexec persona-link \"$@\"\n" + strings.Repeat(" ", 100)),
		"truncated": fakePE(peSubsystemConsole)[:0x90],
		"no PE":     append([]byte("MZ"), make([]byte, 0x200)...),
	} {
		before := string(bad)
		if err := setSubsystem(bad, peSubsystemConsole, peSubsystemGUI); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if string(bad) != before {
			t.Errorf("%s: changed", name)
		}
	}
}

func TestGUICopy(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "persona-link.exe"), filepath.Join(dir, serviceExe)
	if err := os.WriteFile(src, fakePE(peSubsystemConsole), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := guiCopy(src, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if subsystemOf(t, b) != peSubsystemGUI {
		t.Fatal("copy is not GUI")
	}
	if a, _ := os.ReadFile(src); subsystemOf(t, a) != peSubsystemConsole {
		t.Fatal("the original changed")
	}
	if err := guiCopy(filepath.Join(dir, "missing.exe"), dst); err == nil {
		t.Fatal("copied a missing file")
	}
}

// The real thing: this test binary is a Go console program like persona-link.exe.
func TestGUICopyOfAGoProgram(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("needs a Windows build")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "gui.exe")
	if err := guiCopy(self, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if subsystemOf(t, b) != peSubsystemGUI {
		t.Fatal("not GUI")
	}
}
