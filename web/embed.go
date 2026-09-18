// Package web holds the MinCode Web UI static assets.
//
// Source lives in web/ as a Vue 3 + TypeScript app. Production assets are
// built by `npm run build` into web/dist and embedded here so `go build`
// stays a single-binary workflow.
package web

import "embed"

//go:embed all:dist
var FS embed.FS
