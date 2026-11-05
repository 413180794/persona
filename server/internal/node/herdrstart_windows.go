package node

import (
	"context"
	"errors"
)

// herdr's server is not started on Windows: the node keeps reporting it missing, as before (herdrstart.go).
func launchHerdrServer(context.Context, string, string) (<-chan error, error) {
	return nil, errors.New("starting herdr is not supported on Windows")
}

func lockHerdrDir(string) (func(), error) { return func() {}, nil }

// herdrPaneShell: herdr on Windows does not read HERDR_AGENT, so nothing types it (hostherdr.go). A var for tests.
var herdrPaneShell = func() string { return "" }
