//go:build !darwin

package node

import "os"

// dataless: only macOS marks files whose content is not here (OneDrive's placeholders on Windows are irregular).
func dataless(os.FileInfo) bool { return false }
