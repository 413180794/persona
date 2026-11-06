//go:build !linux

package extension

import (
	"os"

	"github.com/nwverse/persona/server/internal/extension/sourcevalidate"
)

func newSourceSnapshot() (*os.File, error) {
	return nil, sourcevalidate.ErrUnavailable
}
func sealSourceSnapshot(*os.File) error { return sourcevalidate.ErrUnavailable }

// Source runtime validation currently requires the independent Linux boundary.
func openSourceFile(root, rel string) (*os.File, error) {
	return nil, sourcevalidate.ErrUnavailable
}
