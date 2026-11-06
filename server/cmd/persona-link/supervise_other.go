//go:build !windows

package main

import "errors"

// supervise is Windows' (supervise_windows.go): elsewhere systemd or launchd restart what exits.
func supervise(*env, []string) error {
	return errors.New("supervise 只在 Windows 上用（计划任务里跑它，见 persona-link install）")
}
