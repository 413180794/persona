//go:build !linux

package node

// Other platforms rely on the app-server's native writer check at thread/resume.
func codexWriterHeld(home, id string) bool { return false }
