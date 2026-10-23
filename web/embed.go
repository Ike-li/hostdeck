// Package web holds the console page, embedded in the binary.
package web

import "embed"

//go:embed index.html app.js connection-help.js style.css i18n
var Files embed.FS
