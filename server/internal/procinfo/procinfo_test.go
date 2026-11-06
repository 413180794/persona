package procinfo

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestOwnProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no parent lookup here")
	}
	if p, err := Parent(os.Getpid()); err != nil || p != os.Getppid() {
		t.Fatalf("parent %d %v, want %d", p, err, os.Getppid())
	}
	if runtime.GOOS == "linux" && !strings.HasPrefix(StartMark(os.Getpid()), "linux:") {
		t.Fatalf("start mark %q", StartMark(os.Getpid()))
	}
}
