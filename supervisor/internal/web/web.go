// Package web serves the built Svelte GUI as static files embedded into the
// supervisor binary, so the whole product ships as one Go binary: a browser on
// the Windows host opens http://<vm-ip>:4001/ and gets the app, which then
// connects to ws://<vm-ip>:4001/control on the same origin.
//
// The dist/ subdirectory is embedded at build time. It ships with a placeholder
// index.html so this package always compiles even before the frontend is built;
// the frontend build (vite build --outDir ../supervisor/internal/web/dist)
// overwrites dist/ with the real bundle.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// distFS embeds the built GUI. The placeholder index.html guarantees the embed
// directive matches at least one file (an empty embed of a missing dir is a
// build error), so the package compiles in a clean checkout.
//
//go:embed dist
var distFS embed.FS

// Handler returns an http.Handler serving the embedded GUI. It serves real
// files with the Content-Type net/http derives from their extension, and falls
// back to index.html for unknown non-file paths so a full-page load of any
// client-side route in the single-page app still renders the app.
//
// This handler is meant to be mounted as the mux's catch-all ("/") AFTER the
// WebSocket routes (/control, /console/) are registered: net/http's ServeMux
// prefers the longer, more specific pattern, so those routes are never shadowed
// by this fallback. The fallback only serves index.html for a path with no
// matching embedded file — it never invents responses for the WS endpoints, and
// the mux precedence keeps them safe regardless.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Only reachable if the embed directive above is broken, which is a
		// build-time programming error, not a runtime condition.
		panic("web: embedded dist/ subtree missing: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean and root-relative the request path for the embedded FS lookup;
		// path.Clean collapses any ".." so a crafted path can't escape dist/.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if fileExists(sub, name) {
			// Real bundle file: FileServer sets Content-Type by extension and
			// handles range/conditional requests.
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: an unknown non-file path is a client-side route, so
		// rewrite to "/" and serve index.html with the FileServer's own
		// text/html Content-Type rather than hand-rolling headers.
		r2 := *r
		u := *r.URL
		u.Path = "/"
		r2.URL = &u
		fileServer.ServeHTTP(w, &r2)
	})
}

// fileExists reports whether name is a regular file in the embedded bundle.
// Directories return false so a bare directory path takes the SPA fallback
// (the app has exactly one HTML entry point) instead of a directory listing.
func fileExists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
