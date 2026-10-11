//go:build tools

// Package tools pins staticcheck for CI. It lives in its own module so the
// tool's dependencies stay out of the CLI's go.mod: staticcheck 2026.2.1
// (v0.8.1) is the newest release and needs a newer golang.org/x/tools than it
// pins itself to read Go 1.27 export data, so `go run ...@v0.8.1` fails.
package tools

import _ "honnef.co/go/tools/cmd/staticcheck"
