// Package web holds the MinCode Web UI static assets.
package web

import "embed"

//go:embed index.html style.css app.js
var FS embed.FS
