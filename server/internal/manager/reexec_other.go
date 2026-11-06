//go:build !unix

package manager

import "errors"

// errNoReexec: this system cannot start the program again in place; Run runs the manager again itself.
var errNoReexec = errors.New("no exec here")

func reexec([]string) error { return errNoReexec }
