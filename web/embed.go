// Package web holds the built web interface. npm run build fills dist, the
// .gitkeep in it lets the Go code compile before that.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func Files() fs.FS {
	files, _ := fs.Sub(dist, "dist")
	return files
}
