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
	store    *Store
	receiver *Receiver
}

type dashboardData struct {
	Config   Config
	Entries  []Entry
	Iface    bool
	Error    string
	Query    string
	Severity string
	Addr     string
}

type settingsData struct {
	Config  Config
	Warning string
}

func NewApp(store *Store) *App {
	cfg := store.Snapshot()
	return &App{store: store, receiver: NewReceiver(cfg.MaxEntries)}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	staticFiles, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /{$}", a.dashboard)
	mux.HandleFunc("GET /settings", a.settings)
	mux.HandleFunc("POST /settings/save", a.saveSettings)
	mux.HandleFunc("POST /clear", a.clear)
	mux.HandleFunc("GET /frag/log", a.logFragment)
	return mux
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	query, maxSeverity, severityText := filterValues(r)
	render(w, "dashboard.html", dashboardData{
		Config:   a.store.Snapshot(),
		Entries:  a.receiver.Filter(query, maxSeverity),
		Iface:    hasLabIface(),
		Error:    a.receiver.LastError(),
		Query:    query,
		Severity: severityText,
		Addr:     a.receiver.Addr(),
	})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	render(w, "settings.html", settingsData{Config: a.store.Snapshot()})
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	oldCfg := a.store.Snapshot()
	cfg := oldCfg
	port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	if err != nil || port < 0 || port > 65535 {
		http.Error(w, "port must be between 0 and 65535", http.StatusBadRequest)
		return
	}
	maxEntries, err := strconv.Atoi(strings.TrimSpace(r.FormValue("max_entries")))
	if err != nil || maxEntries < 1 {
		http.Error(w, "ring size must be at least 1", http.StatusBadRequest)
		return
	}
	cfg.ListenPort = port
	cfg.MaxEntries = maxEntries
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.receiver.Restart(cfg.ListenPort); err != nil {
		warning := "UDP listener was not restarted: " + err.Error()
		if restoreErr := a.store.Save(oldCfg); restoreErr != nil {
			warning += "; restoring saved settings also failed: " + restoreErr.Error()
		}
		render(w, "settings.html", settingsData{Config: oldCfg, Warning: warning})
		return
	}
	a.receiver.Resize(cfg.MaxEntries)
	render(w, "settings.html", settingsData{Config: cfg})
}

func (a *App) clear(w http.ResponseWriter, r *http.Request) {
	a.receiver.Clear()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) logFragment(w http.ResponseWriter, r *http.Request) {
	query, maxSeverity, _ := filterValues(r)
	render(w, "log.html", a.receiver.Filter(query, maxSeverity))
}

func filterValues(r *http.Request) (query string, maxSeverity int, severityText string) {
	query = r.URL.Query().Get("q")
	severityText = r.URL.Query().Get("sev")
	maxSeverity = -1
	if severityText != "" {
		if parsed, err := strconv.Atoi(severityText); err == nil && parsed >= 0 && parsed <= 7 {
			maxSeverity = parsed
		} else {
			severityText = ""
		}
	}
	return query, maxSeverity, severityText
}

func render(w http.ResponseWriter, page string, data any) {
	t, err := template.New("layout").Funcs(template.FuncMap{
		"severityName":  severityName,
		"severityClass": severityClass,
	}).ParseFS(assets, "templates/layout.html", "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
