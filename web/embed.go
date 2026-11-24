// Package web holds the browser UI, a mobile-first progressive web app with
// no build step: the files here are served as they are, embedded in
// sepianod.
package web

import "embed"

// Files is the UI, served at the root of the HTTP server.
//
//go:embed index.html app.js editor.js style.css manifest.webmanifest sw.js icon.svg
var Files embed.FS
