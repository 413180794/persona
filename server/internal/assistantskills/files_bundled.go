//go:build !public

package assistantskills

import "embed"

//go:embed all:lark-connector
var bundled embed.FS
