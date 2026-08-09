package main

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

// The assets are embedded so the installed pack is one self-contained binary.
//
//go:embed templates/*.html static/*
var assets embed.FS

type App struct {
	store  *Store
	radius *RadiusServer
}

func NewApp(store *Store) *App {
	return &App{store: store, radius: NewRadiusServer(store)}
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
	mux.HandleFunc("POST /users/add", a.addUser)
	mux.HandleFunc("POST /users/delete", a.deleteUser)
	mux.HandleFunc("GET /frag/log", a.logFragment)
	return mux
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Config   Config
		Attempts []AuthAttempt
		Iface    bool
	}{a.store.Snapshot(), a.radius.Attempts(), hasLabIface()}
	render(w, "dashboard.html", data)
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	render(w, "settings.html", a.store.Snapshot())
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := a.store.Snapshot()
	cfg.SharedSecret = r.FormValue("shared_secret")
	protocol := strings.ToLower(strings.TrimSpace(r.FormValue("protocol")))
	if protocol == "radius" || protocol == "tacacs" {
		cfg.Protocol = protocol
	}
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *App) addUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	priv, _ := strconv.Atoi(r.FormValue("priv_lvl"))
	cfg := a.store.Snapshot()
	cfg.Users = append(cfg.Users, User{Username: r.FormValue("username"), Password: r.FormValue("password"), Service: r.FormValue("service"), PrivLvl: priv})
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	index, err := strconv.Atoi(r.FormValue("index"))
	cfg := a.store.Snapshot()
	if err != nil || index < 0 || index >= len(cfg.Users) {
		http.Error(w, "invalid user index", http.StatusBadRequest)
		return
	}
	cfg.Users = append(cfg.Users[:index], cfg.Users[index+1:]...)
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *App) logFragment(w http.ResponseWriter, r *http.Request) {
	render(w, "log.html", a.radius.Attempts())
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
