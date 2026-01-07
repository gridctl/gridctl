//go:build embed_web

package main

import (
	"embed"
	"io/fs"
)

// The web/dist directory is copied here during build (see Makefile).
// This file is only included when building with -tags embed_web.
//
//go:embed all:web/dist
var embeddedFiles embed.FS

// WebFS returns the embedded web files as a filesystem.
func WebFS() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "web/dist")
}
