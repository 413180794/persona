//go:build !unix

package hubedge

import (
	"errors"
	"io"
)

func slotPipes() (io.Reader, io.Writer, error) {
	return nil, nil, errors.New("hubedge: the edge runs on Linux and macOS only")
}
