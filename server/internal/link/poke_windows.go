//go:build windows

package link

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

// WatchPokes calls check or redial each time the event of PokeCheck or PokeRedial for the link.json at path is set,
// until ctx ends. The events reset themselves, so pokes arriving close together are one.
func WatchPokes(ctx context.Context, path string, check, redial func()) error {
	var handles []windows.Handle
	for _, kind := range []string{PokeCheck, PokeRedial} {
		name, err := windows.UTF16PtrFromString(PokeEventName(path, kind))
		if err != nil {
			return err
		}
		h, err := windows.CreateEvent(nil, 0, 0, name)
		if err != nil {
			for _, h := range handles {
				windows.CloseHandle(h)
			}
			return fmt.Errorf("event %s: %w", kind, err)
		}
		handles = append(handles, h)
	}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
		return err
	}
	handles = append(handles, stop)
	cancel := context.AfterFunc(ctx, func() { windows.SetEvent(stop) })
	go func() {
		defer func() {
			cancel()
			for _, h := range handles {
				windows.CloseHandle(h)
			}
		}()
		for {
			r, err := windows.WaitForMultipleObjects(handles, false, windows.INFINITE)
			if err != nil {
				return
			}
			switch r - windows.WAIT_OBJECT_0 {
			case 0:
				check()
			case 1:
				redial()
			default:
				return
			}
		}
	}()
	return nil
}

// Poke sets the event of kind for the persona-link of the link.json at path; an error when none is running there.
func Poke(path, kind string) error {
	name, err := windows.UTF16PtrFromString(PokeEventName(path, kind))
	if err != nil {
		return err
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return fmt.Errorf("no Persona Link is running to poke: %w", err)
	}
	defer windows.CloseHandle(h)
	return windows.SetEvent(h)
}
