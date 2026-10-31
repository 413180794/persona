//go:build public

package assistantskills

import "embed"

// A public build ships no copied skills (embed.go).
var bundled embed.FS
