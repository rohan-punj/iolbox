package main

import (
	"embed"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/*
var staticFS embed.FS

type App struct {
	store *Store
	sup   *Supervisor
	tmpl  *template.Template
}

func logf(format string, a ...any) { log.Printf(format, a...) }

func main() {
	configPath := os.Getenv("IOLBOX_TOOL_OPTIONS")
	socketPath := os.Getenv("IOLBOX_TOOL_SOCK")
	if configPath == "" || socketPath == "" {
		log.Fatal("IOLBOX_TOOL_OPTIONS and IOLBOX_TOOL_SOCK must be set")
	}

	store := NewStore(configPath)
	_ = store.Update(func(c *Config) {
		if c.ReconSubnet == "" {
			c.ReconSubnet = deriveReconSubnet()
		}
	})

	sup := newSupervisor()
	app := &App{
		store: store,
		sup:   sup,
		tmpl:  template.Must(template.New("").Funcs(funcMap()).ParseFS(tmplFS, "templates/*.html")),
	}

	mux := http.NewServeMux()
	app.routes(mux)
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("remove stale socket: %v", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("listen on %s: %v", socketPath, err)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		log.Fatalf("chmod socket: %v", err)
	}
	logf("Security Bench GUI listening on unix socket %s (config %s, lab iface %s)", socketPath, configPath, labIface)
	if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"join":   strings.Join,
		"hasSub": strings.Contains,
		"dict": func(pairs ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(pairs); i += 2 {
				if k, ok := pairs[i].(string); ok {
					m[k] = pairs[i+1]
				}
			}
			return m
		},
	}
}
