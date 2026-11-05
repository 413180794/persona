//go:build !windows

package node

import "errors"

// winProcs and winListeners are Windows' port scan (ports_windows.go); Linux reads /proc, macOS runs ps and lsof.
func winProcs() (map[int]proc, error) { return nil, errors.New("no Windows process table here") }

func winListeners([]int) ([]listener, error) { return nil, errors.New("no Windows TCP table here") }
