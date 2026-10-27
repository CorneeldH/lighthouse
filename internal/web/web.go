// Package web embeds the built single-page app, or a placeholder when it is not built.
package web

import (
	"embed"
	"io/fs"
)

// all: keeps dist/.gitkeep embeddable while dist holds nothing else.
//
//go:embed all:dist placeholder
var embedded embed.FS

// Choose returns dist when root holds a built app (dist/index.html), else placeholder.
func Choose(root fs.FS) fs.FS {
	dir := "placeholder"
	if _, err := fs.Stat(root, "dist/index.html"); err == nil {
		dir = "dist"
	}
	site, err := fs.Sub(root, dir)
	if err != nil {
		panic("web: " + dir + " directory missing: " + err.Error())
	}
	return site
}

// FS returns the embedded site: the built app, or the placeholder.
func FS() fs.FS {
	return Choose(embedded)
}
