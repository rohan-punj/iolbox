package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

type FetchRequest struct {
	URL     string
	Method  string
	Headers string
	Body    string
}

type FetchResult struct {
	StatusCode int
	Status     string
	Headers    http.Header
	Body       string
}

func Fetch(request FetchRequest) (FetchResult, error) {
	return fetchWithTimeout(request, defaultTimeout)
}

func fetchWithTimeout(input FetchRequest, timeout time.Duration) (FetchResult, error) {
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead:
	default:
		return FetchResult{}, fmt.Errorf("unsupported HTTP method %q", input.Method)
	}
	parsed, err := url.Parse(strings.TrimSpace(input.URL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return FetchResult{}, errors.New("URL must be an absolute http:// or https:// URL")
	}
	headers, err := parseHeaders(input.Headers)
	if err != nil {
		return FetchResult{}, err
	}
	request, err := http.NewRequestWithContext(context.Background(), method, parsed.String(), strings.NewReader(input.Body))
	if err != nil {
		return FetchResult{}, err
	}
	request.Header = headers
	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		return FetchResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return FetchResult{}, err
	}
	return FetchResult{StatusCode: response.StatusCode, Status: response.Status, Headers: response.Header.Clone(), Body: string(body)}, nil
}

func parseHeaders(raw string) (http.Header, error) {
	headers := make(http.Header)
	for lineNumber, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("header line %d must be Key: Value", lineNumber+1)
		}
		headers.Add(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	return headers, nil
}
