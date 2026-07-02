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
	if !strings.Contains(string(body), "iolab") {
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
	if !strings.Contains(string(body), "iolab") {
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
	if !strings.Contains(string(body), "iolab") {
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
	if !strings.Contains(string(body), "iolab") {
		t.Fatalf("traversal did not resolve to index.html: %q", body)
	}
}
