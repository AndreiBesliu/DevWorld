// Package tools pins the developer tooling used by CI and by local runs.
//
// The tools are declared as tool directives in go.mod and run from the ipcam
// directory with "go tool golangci-lint", "go tool govulncheck" and
// "go tool go-licenses". They are never linked into shipped binaries.
package tools
