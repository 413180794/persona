//go:build !linux && !darwin

package assistant

// freeBytes: not told on this system (the copy goes on without checking).
func freeBytes(string) (int64, bool) { return 0, false }
