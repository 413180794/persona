//go:build !linux && !windows

package netaddr

import "context"

func watchEvents(context.Context) <-chan struct{} { return nil }
