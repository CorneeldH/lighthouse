package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"time"
)

// spa serves files from site and falls back to index.html for every other path.
func spa(site fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean("/" + r.URL.Path)[1:]
		if name != "" {
			if info, err := fs.Stat(site, name); err == nil && !info.IsDir() {
				http.ServeFileFS(w, r, site, name)
				return
			}
		}
		index, err := fs.ReadFile(site, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
