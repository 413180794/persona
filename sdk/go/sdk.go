// Package sdk carries the SDK's own files, for `personad extension new` (a new extension gets a copy of the SDK it is
// written against) and scripts/gen/sync-sdk.sh: the Go SDK as it is here (ext, exttest, wire), and bundle/sdk.tar.gz
// with the TypeScript SDK and the templates (scripts/gen/sdk_bundle.py makes it; go:embed cannot reach outside
// this module).
package sdk

import "embed"

// Go are the Go SDK's packages, as files.
//
//go:embed ext/*.go exttest/*.go wire/*.go wire/schema
var Go embed.FS

// Bundle is the TypeScript SDK (ts/…) and the templates (templates/<name>/…), a gzipped tar.
//
//go:embed bundle/sdk.tar.gz
var Bundle []byte

// GoMod is the go.mod of a copy of the Go SDK.
const GoMod = "module github.com/nwverse/persona/sdk/go\n\ngo 1.24\n"
