//go:build !linux && !darwin

package procinfo

// Parent: not known here.
func Parent(int) (int, error) { return 0, ErrUnknown }
