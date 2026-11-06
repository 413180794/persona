package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// guiCopy writes a copy of the Windows program at src to dst that runs without a console window: the same bytes with
// the PE optional header's Subsystem changed from console (3) to GUI (2). Task Scheduler gives a console program
// started in the user's session a console window of its own (a Windows Terminal one where that is the default
// terminal); a GUI one gets none. The service's supervisor runs from this copy, and starts persona-link and the node
// with CREATE_NO_WINDOW (winservice.go). A copy rather than a second build: whatever persona-link.exe is installed,
// from a release or from `go build`, the supervisor is the same program.
func guiCopy(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := setSubsystem(b, peSubsystemConsole, peSubsystemGUI); err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

const (
	peSubsystemGUI     = 2
	peSubsystemConsole = 3
)

// setSubsystem changes a PE image's Subsystem field from want to to; it refuses anything that is not a PE image with
// Subsystem want. The field is at offset 68 of the optional header in both PE32 and PE32+.
func setSubsystem(b []byte, want, to uint16) error {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return errors.New("not a Windows program (no MZ header)")
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	const coff = 20
	opt := pe + 4 + coff
	if pe <= 0 || opt+70 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
		return errors.New("not a Windows program (no PE header)")
	}
	if size := int(binary.LittleEndian.Uint16(b[pe+4+16:])); size < 70 {
		return errors.New("not a Windows program (optional header too short)")
	}
	if magic := binary.LittleEndian.Uint16(b[opt:]); magic != 0x10b && magic != 0x20b {
		return fmt.Errorf("not a Windows program (optional header magic %#x)", magic)
	}
	field := b[opt+68:]
	if got := binary.LittleEndian.Uint16(field); got != want {
		return fmt.Errorf("subsystem is %d, want %d", got, want)
	}
	binary.LittleEndian.PutUint16(field, to)
	return nil
}
