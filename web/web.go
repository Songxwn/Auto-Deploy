// Package web embeds the single-page frontend into the binary.
package web

import "embed"

// FS contains the static frontend assets.
//
//go:embed index.html app.js style.css
var FS embed.FS
