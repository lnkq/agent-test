// Package frontend serves the embedded request-testing UI. It is static
// vanilla JS (no build step, no external/CDN assets) that sends requests
// through the gateway and displays the response, the chosen upstream, and
// live counters plus a canary-split chart.
package frontend

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var webFS embed.FS

// Handler serves the embedded frontend assets.
func Handler() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err) // cannot happen: directory is embedded
	}
	return http.FileServer(http.FS(sub))
}
