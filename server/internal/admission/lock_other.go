//go:build !linux && !darwin

package admission

import "errors"

func checkDir(string) error {
	return errors.New("persistent admission is available on Linux and macOS")
}
func lockDir(dir string) (handle, error) { return handle{}, checkDir(dir) }
