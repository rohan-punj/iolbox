package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchPreservesRawResponseAndNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Lab-Test") != "yes" {
			t.Error("custom header did not reach server")
		}
		w.Header().Set("X-Response", "known-header")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("<raw>&body"))
	}))
	defer server.Close()
	result, err := Fetch(FetchRequest{URL: server.URL, Method: http.MethodGet, Headers: "X-Lab-Test: yes"})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusTeapot || !strings.Contains(result.Status, "418") {
		t.Fatalf("status = %d %q", result.StatusCode, result.Status)
	}
	if result.Headers.Get("X-Response") != "known-header" {
		t.Fatalf("headers = %+v", result.Headers)
	}
	if result.Body != "<raw>&body" {
		t.Fatalf("body = %q", result.Body)
	}
}

func TestFetchTimeoutIsSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	_, err := fetchWithTimeout(FetchRequest{URL: server.URL, Method: http.MethodGet}, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestParseHeaders(t *testing.T) {
	headers, err := parseHeaders("Accept: application/json\nX-Test: two: values")
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("Accept") != "application/json" || headers.Get("X-Test") != "two: values" {
		t.Fatalf("headers = %+v", headers)
	}
}
