//go:build !embed_web

package main

import (
	"io/fs"
)

// WebFS returns nil when built without embedded web files.
// Use -tags embed_web to include the web UI.
func WebFS() (fs.FS, error) {
	return nil, nil
}
