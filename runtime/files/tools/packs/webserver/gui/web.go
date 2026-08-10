package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AccessLog struct {
	At       time.Time
	Method   string
	Path     string
	Status   int
	RemoteIP string
}

type accessRing struct {
	mu    sync.RWMutex
	items []AccessLog
	max   int
}

func newAccessRing(max int) *accessRing {
	if max < 1 {
		max = 100
	}
	return &accessRing{max: max}
}

func (r *accessRing) Add(item AccessLog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
	if len(r.items) > r.max {
		r.items = append([]AccessLog(nil), r.items[len(r.items)-r.max:]...)
	}
}

func (r *accessRing) List() []AccessLog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AccessLog(nil), r.items...)
}

type WebService struct {
	mu       sync.RWMutex
	cfg      Config
	server   *http.Server
	listener net.Listener
	logs     *accessRing
}

func NewWeb(cfg Config) *WebService {
	if cfg.ExtraPaths == nil {
		cfg.ExtraPaths = map[string]string{}
	}
	return &WebService{cfg: cloneConfig(cfg), logs: newAccessRing(100)}
}

func (w *WebService) Serve() error {
	w.mu.RLock()
	port := w.cfg.ListenPort
	w.mu.RUnlock()
	return w.serve(port, nil)
}

func (w *WebService) serve(port int, ready chan<- error) error {
	if port < 0 || port > 65535 {
		err := fmt.Errorf("port %d is out of range (must be 0-65535)", port)
		if ready != nil {
			ready <- err
		}
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
	if err != nil {
		if ready != nil {
			ready <- err
		}
		return err
	}
	server := &http.Server{Handler: w.handler()}
	w.mu.Lock()
	w.listener = listener
	w.server = server
	w.mu.Unlock()
	if ready != nil {
		ready <- nil
	}
	err = server.Serve(listener)
	w.mu.Lock()
	if w.server == server {
		w.server = nil
		w.listener = nil
	}
	w.mu.Unlock()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (w *WebService) Restart(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("port %d is out of range (must be 0-65535)", port)
	}
	w.mu.RLock()
	old := w.server
	w.mu.RUnlock()
	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = old.Shutdown(ctx)
		cancel()
	}
	w.mu.Lock()
	w.cfg.ListenPort = port
	w.mu.Unlock()
	ready := make(chan error, 1)
	go func() { _ = w.serve(port, ready) }()
	return <-ready
}

func (w *WebService) Close() error {
	w.mu.RLock()
	server := w.server
	w.mu.RUnlock()
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func (w *WebService) Addr() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.listener == nil {
		return ""
	}
	return w.listener.Addr().String()
}

func (w *WebService) Logs() []AccessLog { return w.logs.List() }

func (w *WebService) Config() Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return cloneConfig(w.cfg)
}

func (w *WebService) SetConfig(cfg Config) {
	w.mu.Lock()
	w.cfg = cloneConfig(cfg)
	w.mu.Unlock()
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *WebService) handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writer := &statusWriter{ResponseWriter: response}
		cfg := w.Config()
		switch {
		case request.URL.Path == "/":
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write([]byte(cfg.IndexHTML))
		case strings.HasPrefix(request.URL.Path, "/"):
			if body, ok := cfg.ExtraPaths[request.URL.Path]; ok {
				response.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = writer.Write([]byte(body))
			} else {
				http.NotFound(writer, request)
			}
		}
		remoteIP := request.RemoteAddr
		if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
			remoteIP = host
		}
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		w.logs.Add(AccessLog{At: time.Now(), Method: request.Method, Path: request.URL.Path, Status: status, RemoteIP: remoteIP})
	})
}
