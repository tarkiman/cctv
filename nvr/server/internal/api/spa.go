package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the built web app; unknown paths get index.html so client-side
// routes work on reload. Hashed assets are cached for a year, index.html never.
func spaHandler(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(web, p); err != nil {
			serveIndex(w, r, web)
			return
		}
		switch {
		case strings.HasPrefix(p, "assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case p == "index.html" || p == "sw.js" || strings.HasSuffix(p, ".webmanifest"):
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, web fs.FS) {
	b, err := fs.ReadFile(web, "index.html")
	if err != nil {
		http.Error(w, "web app not built", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
