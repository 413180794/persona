//go:build !unix

package main

// notifySignals: there is no SIGUSR1 / SIGUSR2 here; jarvis-link still notices a changed network by itself
// (heartbeats, netaddr.Watch's poll).
func notifySignals(check, redial func()) {}
