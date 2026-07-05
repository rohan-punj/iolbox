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
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
	etags := computeETags(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean and root-relative the request path for the embedded FS lookup;
		// path.Clean collapses any ".." so a crafted path can't escape dist/.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if fileExists(sub, name) {
			setCacheHeaders(w, name, etags)
			// Real bundle file: FileServer sets Content-Type by extension and
			// handles range/conditional requests (the ETag set above is picked
			// up by net/http's precondition checks, so If-None-Match => 304).
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: an unknown non-file path is a client-side route, so
		// rewrite to "/" and serve index.html with the FileServer's own
		// text/html Content-Type rather than hand-rolling headers. Cache policy
		// is index.html's (always revalidate) — a stale cached copy of the app
		// shell after an upgrade is the one failure mode this must prevent.
		setCacheHeaders(w, "index.html", etags)
		r2 := *r
		u := *r.URL
		u.Path = "/"
		r2.URL = &u
		fileServer.ServeHTTP(w, &r2)
	})
}

// computeETags walks the embedded bundle once at startup and derives a strong
// content ETag (sha256 prefix) per file. embed.FS files carry NO modtime, so
// net/http on its own can emit neither Last-Modified nor ETag for them — which
// is why, before this, the GUI shipped with no validators at all: every load
// re-downloaded the full bundle, and nothing forced a browser holding a
// heuristically-cached index.html to notice an upgraded binary. The bundle is
// well under a few MB, so hashing it once at Handler() construction is
// negligible.
func computeETags(fsys fs.FS) map[string]string {
	etags := make(map[string]string)
	_ = fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // an unreadable entry just goes without an ETag
		}
		b, rerr := fs.ReadFile(fsys, name)
		if rerr != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		etags[name] = `"` + hex.EncodeToString(sum[:8]) + `"`
		return nil
	})
	return etags
}

// setCacheHeaders applies the SPA caching split:
//
//   - assets/*: Vite content-hashes every filename in assets/ (index-<hash>.js
//     etc.), so a given URL's bytes can never change — cache it for a year,
//     immutable. An upgraded binary ships new hashes under new URLs, so stale
//     reuse is structurally impossible.
//   - everything else (index.html, favicon, manifest — stable, un-hashed
//     names): no-cache, meaning the browser MAY store it but MUST revalidate
//     before use. Paired with the strong ETag this costs one conditional
//     request answered 304 while the binary is unchanged, and guarantees the
//     first load after a redeploy picks up the new app shell (and thus the new
//     asset hashes) instead of white-screening on vanished bundles.
func setCacheHeaders(w http.ResponseWriter, name string, etags map[string]string) {
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if tag, ok := etags[name]; ok {
		w.Header().Set("ETag", tag)
	}
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
