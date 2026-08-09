package main

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

//go:embed templates/*.html static/*
var assets embed.FS

type App struct {
	store *Store
	web   *WebService
}

func NewApp(store *Store) *App { return &App{store: store, web: NewWeb(store.Snapshot())} }

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	staticFiles, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK); _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /{$}", a.dashboard)
	mux.HandleFunc("GET /settings", a.settings)
	mux.HandleFunc("POST /settings/save", a.saveSettings)
	mux.HandleFunc("GET /frag/log", a.logFragment)
	return mux
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Config Config
		Addr   string
		Logs   []AccessLog
		Iface  bool
	}{a.store.Snapshot(), a.web.Addr(), a.web.Logs(), hasLabIface()}
	render(w, "dashboard.html", data)
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	render(w, "settings.html", struct {
		Config  Config
		Warning string
	}{a.store.Snapshot(), ""})
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := a.store.Snapshot()
	port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	if err != nil || port < 0 || port > 65535 {
		http.Error(w, "port must be between 0 and 65535", http.StatusBadRequest)
		return
	}
	cfg.ListenPort = port
	cfg.IndexHTML = r.FormValue("index_html")
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.web.SetConfig(cfg)
	warning := ""
	if port > 0 && port < 1024 {
		warning = "Ports below 1024 require the privileged-port supervisor enablement; the listener was not restarted."
	} else if err := a.web.Restart(port); err != nil {
		warning = err.Error()
	}
	render(w, "settings.html", struct {
		Config  Config
		Warning string
	}{cfg, warning})
}

func (a *App) logFragment(w http.ResponseWriter, r *http.Request) {
	render(w, "log.html", a.web.Logs())
}

func render(w http.ResponseWriter, page string, data any) {
	t, err := template.ParseFS(assets, "templates/layout.html", "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
