//go:build unix

package hubedge

import (
	"io"
	"os"
	"syscall"
)

// slotPipes are the fds the edge passed (ExtraFiles), made close-on-exec: the nodes, plugins and agents the slot
// starts must not hold the edge's pipes open (the edge would never see this slot go away).
func slotPipes() (io.Reader, io.Writer, error) {
	syscall.CloseOnExec(slotReadFD)
	syscall.CloseOnExec(slotWriteFD)
	return os.NewFile(slotReadFD, "edge-in"), os.NewFile(slotWriteFD, "edge-out"), nil
}
