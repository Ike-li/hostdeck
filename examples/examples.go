// Package examples holds plugins that work as they are, or after setting the
// address or key that each one's description names. They are built into
// hostdeck, and its plugins page adds them to the plugins folder.
package examples

import "embed"

// Files holds each example's folder; the Go files beside them are left out.
//
//go:embed */*
var Files embed.FS
