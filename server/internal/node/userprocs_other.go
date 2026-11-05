//go:build !linux

package node

// readProcArgv: there is no procfs to walk off Linux.
func readProcArgv(string, uint32) map[int][]string { return nil }
