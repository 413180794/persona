//go:build !windows

package main

import (
	"errors"
	"time"
)

// The tray is Windows' (tray_windows.go); the Mac has its menu bar app, Linux servers have no taskbar.

func trayCmd(*env, []string) error {
	return errors.New("托盘图标只在 Windows 上有：Mac 上是菜单栏 App「Persona Link」，Linux 上看 persona-link status")
}

var errNoTray = errors.New("no tray here")

// systemTray: nothing to touch; install and uninstall only reach it on Windows.
func systemTray() trayControl {
	return trayControl{
		setAutostart:    func(string, bool) error { return errNoTray },
		autostart:       func() bool { return false },
		registerLinks:   func(string) error { return errNoTray },
		unregisterLinks: func() {},
		start:           func(string) error { return errNoTray },
		quit:            func(string, time.Duration) bool { return true },
		running:         func(string) bool { return false },
		removeData:      func() error { return nil },
	}
}
