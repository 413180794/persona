//go:build unix

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// notifySignals: SIGUSR1 (the network changed) calls check, SIGUSR2 (reconnect now) calls redial. The menu bar app
// sends them to its helper.
func notifySignals(_ context.Context, _ string, check, redial func()) {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		for s := range sigs {
			if s == syscall.SIGUSR2 {
				redial()
			} else {
				check()
			}
		}
	}()
}
