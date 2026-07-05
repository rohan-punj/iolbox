package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// get issues a plain GET against the handler and returns the recorder.
func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	h.ServeHTTP(rr, req)
	return rr
}

func TestServesIndexAtRoot(t *testing.T) {
	rr := get(t, Handler(), "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / Content-Type = %q, want text/html*", ct)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "iolbox") {
		t.Fatalf("GET / body missing placeholder marker: %q", body)
	}
}

func TestServesIndexHTMLFileDirectly(t *testing.T) {
	// net/http's FileServer canonicalizes "/index.html" with a 301 redirect to
	// "/" (its documented behavior); browsers follow it and land on the app.
	// The point of this test is that the file is recognized as a real bundle
	// file (redirect), not treated as an unknown route (which would 200 the SPA
	// fallback with no redirect) — either way it never 404s.
	rr := get(t, Handler(), "/index.html")
	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /index.html status = %d, want 301 to /", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "./" && loc != "/" {
		t.Fatalf("GET /index.html Location = %q, want redirect to root", loc)
	}
}

func TestUnknownPathFallsBackToIndex(t *testing.T) {
	// A client-side route with no matching embedded file must render the app
	// (index.html) with a 200, not a 404, so deep-link full-page loads work.
	rr := get(t, Handler(), "/some/spa/route")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /some/spa/route status = %d, want 200 (SPA fallback)", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("SPA fallback Content-Type = %q, want text/html*", ct)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "iolbox") {
		t.Fatalf("SPA fallback did not serve index.html: %q", body)
	}
}

func TestUnknownAssetExtensionStillFallsBack(t *testing.T) {
	// A request that looks like an asset but isn't in the bundle also falls
	// back to index.html rather than 404 (the app decides what to do).
	rr := get(t, Handler(), "/assets/does-not-exist.js")
	if rr.Code != http.StatusOK {
		t.Fatalf("missing asset status = %d, want 200 (fallback)", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "iolbox") {
		t.Fatalf("missing asset did not fall back to index.html: %q", body)
	}
}

func TestPathTraversalIsContained(t *testing.T) {
	// A traversal attempt is cleaned to a rooted path and cannot escape dist/;
	// it resolves to a non-file and takes the SPA fallback (200 index.html).
	rr := get(t, Handler(), "/../../etc/passwd")
	if rr.Code != http.StatusOK {
		t.Fatalf("traversal status = %d, want 200 (contained fallback)", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "iolbox") {
		t.Fatalf("traversal did not resolve to index.html: %q", body)
	}
}

func TestIndexCacheHeadersAndConditionalGet(t *testing.T) {
	h := Handler()
	rr := get(t, h, "/")
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("GET / Cache-Control = %q, want no-cache (app shell must revalidate)", cc)
	}
	tag := rr.Header().Get("ETag")
	if tag == "" {
		t.Fatal("GET / has no ETag; embed.FS carries no modtime, so without our ETag there is no validator at all")
	}

	// Revalidation with the same ETag must be answered 304 with no body.
	rr2 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", tag)
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusNotModified {
		t.Fatalf("conditional GET / with matching ETag = %d, want 304", rr2.Code)
	}

	// SPA fallback carries index.html's policy: a stale cached shell after an
	// upgrade is the failure mode no-cache exists to prevent.
	rr3 := get(t, h, "/some/spa/route")
	if cc := rr3.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("SPA fallback Cache-Control = %q, want no-cache", cc)
	}
	if rr3.Header().Get("ETag") != tag {
		t.Fatalf("SPA fallback ETag = %q, want index.html's %q", rr3.Header().Get("ETag"), tag)
	}
}

func TestSetCacheHeadersSplitsAssetsFromShell(t *testing.T) {
	// assets/* filenames are content-hashed by Vite, so the same URL can never
	// serve different bytes: cache forever, immutable. Everything else keeps a
	// stable name across releases and must always revalidate.
	etags := map[string]string{
		"assets/index-C-hazZTN.js": `"aa"`,
		"index.html":               `"bb"`,
	}
	rr := httptest.NewRecorder()
	setCacheHeaders(rr, "assets/index-C-hazZTN.js", etags)
	if cc := rr.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("assets Cache-Control = %q, want immutable year-long", cc)
	}
	if rr.Header().Get("ETag") != `"aa"` {
		t.Fatalf("assets ETag = %q, want map value", rr.Header().Get("ETag"))
	}

	rr2 := httptest.NewRecorder()
	setCacheHeaders(rr2, "favicon.svg", etags)
	if cc := rr2.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("non-asset Cache-Control = %q, want no-cache", cc)
	}
	// No ETag entry for favicon.svg in the map: header simply absent, not bogus.
	if rr2.Header().Get("ETag") != "" {
		t.Fatalf("unexpected ETag %q for un-hashed file with no map entry", rr2.Header().Get("ETag"))
	}
}
