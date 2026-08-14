package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type responseStep struct {
	status int
	err    error
}

type scriptedTransport struct {
	mu    sync.Mutex
	steps []responseStep
}

func (t *scriptedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.steps) == 0 {
		return nil, errors.New("script exhausted")
	}
	step := t.steps[0]
	t.steps = t.steps[1:]
	if step.err != nil {
		return nil, step.err
	}
	return &http.Response{StatusCode: step.status, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func TestWaitHTTPReadyAcceptsLivenessStatusesAfterRefusal(t *testing.T) {
	client := &http.Client{Transport: &scriptedTransport{steps: []responseStep{
		{err: errors.New("connection refused")}, {status: http.StatusInternalServerError}, {status: http.StatusNotFound},
	}}}
	status, err := waitHTTPReady(t.Context(), client, "http://liveness.invalid/", time.Second)
	if err != nil || status != http.StatusNotFound {
		t.Fatalf("status/error = %d/%v", status, err)
	}
}

func TestWaitHTTPReadyTimeoutsOnFiveHundred(t *testing.T) {
	client := &http.Client{Transport: &scriptedTransport{steps: []responseStep{{status: http.StatusInternalServerError}}}}
	status, err := waitHTTPReady(t.Context(), client, "http://liveness.invalid/", 260*time.Millisecond)
	if err == nil || status != http.StatusInternalServerError {
		t.Fatalf("status/error = %d/%v", status, err)
	}
}

func TestWaitHTTPReadyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitHTTPReady(ctx, &http.Client{Transport: &scriptedTransport{steps: []responseStep{{err: errors.New("refused")}}}}, "http://liveness.invalid/", time.Second)
	if err == nil {
		t.Fatal("cancelled readiness unexpectedly succeeded")
	}
}
