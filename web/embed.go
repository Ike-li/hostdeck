// Package web holds the console page, embedded in the binary.
package web

import "embed"

//go:embed index.html app.js state.js dom.js api.js hooks.js i18n.js panel.js nav.js confirm.js frag.js layout.js plugins.js overview.js monitor.js observations.js tasks.js disk-history.js service.js catalog.js editor.js notify.js dnd.js connection-help.js style.css i18n
var Files embed.FS
