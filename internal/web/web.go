// Package web embeds the built single-page app.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// FS returns the embedded site rooted at dist.
func FS() fs.FS {
	site, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: embedded dist directory missing: " + err.Error())
	}
	return site
}
