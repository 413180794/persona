//go:build !unix

package manager

// lockFile does nothing here: the built-in manager's own service is a Linux and macOS thing (systemd, launchd).
func lockFile(string) error { return nil }
