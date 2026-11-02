package node

import (
	"os"
	"testing"
)

func TestMachineUIDIsMadeOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadMachineUID(dir)
	if err != nil || !uidPattern.MatchString(first) {
		t.Fatalf("uid %q, %v", first, err)
	}
	st, err := os.Stat(MachineUIDPath(dir))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v, %v", st.Mode().Perm(), err)
	}
	again, err := LoadMachineUID(dir)
	if err != nil || again != first {
		t.Fatalf("second load %q, want %q (%v)", again, first, err)
	}
	reset, err := ResetMachineUID(dir)
	if err != nil || reset == first || !uidPattern.MatchString(reset) {
		t.Fatalf("reset %q (old %q), %v", reset, first, err)
	}
	if now, _ := LoadMachineUID(dir); now != reset {
		t.Fatalf("after reset %q, want %q", now, reset)
	}
}

func TestMachineUIDReplacesGarbage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(MachineUIDPath(dir), []byte("not-a-uuid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uid, err := LoadMachineUID(dir)
	if err != nil || !uidPattern.MatchString(uid) {
		t.Fatalf("uid %q, %v", uid, err)
	}
}
