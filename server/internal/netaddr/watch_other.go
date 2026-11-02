//go:build !linux

package netaddr

import "context"

func watchEvents(context.Context) <-chan struct{} { return nil }
