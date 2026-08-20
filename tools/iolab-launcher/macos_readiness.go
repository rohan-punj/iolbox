package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func waitHTTPReady(ctx context.Context, client *http.Client, url string, timeout time.Duration) (int, error) {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastStatus int
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		resp, err := client.Do(req)
		if err == nil {
			lastStatus = resp.StatusCode
			_, _ = resp.Body.Read(make([]byte, 1))
			_ = resp.Body.Close()
			if resp.StatusCode < http.StatusInternalServerError {
				return resp.StatusCode, nil
			}
			lastErr = fmt.Errorf("HTTP status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastStatus != 0 {
				return lastStatus, fmt.Errorf("readiness timeout after HTTP status %d: %w", lastStatus, ctx.Err())
			}
			return 0, fmt.Errorf("readiness timeout: %v (last probe: %w)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}
