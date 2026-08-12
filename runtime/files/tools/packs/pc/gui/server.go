package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
)

//go:embed templates/*.html static/*
var assets embed.FS

type App struct {
	store *Store
	state *RuntimeState
	flows *FlowManager
	socks *SocketManager
}

func NewApp(store *Store) *App {
	return &App{store: store, state: NewRuntimeState(store), flows: NewFlowManager(), socks: NewSocketManager()}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	staticFiles, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /{$}", a.dashboard)
	mux.HandleFunc("GET /settings", a.settings)
	mux.HandleFunc("POST /settings/save", a.saveSettings)
	mux.HandleFunc("GET /_iolbox/state", a.stateEndpoint)
	mux.HandleFunc("POST /_iolbox/state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
	})
	return mux
}

func (a *App) dashboard(w http.ResponseWriter, _ *http.Request) {
	addr, lease, cfg := a.state.Snapshot()
	render(w, "dashboard.html", struct {
		Address Address
		Lease   *Lease
		Config  Config
		Iface   bool
	}{addr, lease, cfg, hasLabIface()})
}

func (a *App) settings(w http.ResponseWriter, _ *http.Request) {
	_, _, cfg := a.state.Snapshot()
	render(w, "settings.html", cfg)
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := a.store.Snapshot()
	cfg.PC.DHCP = r.FormValue("dhcp") == "on"
	cfg.PC.SavedCommands = splitSavedCommands(r.FormValue("saved_commands"))
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ip := r.FormValue("ip"); ip != "" {
		parsed, prefix, gateway, err := parseAddressForm(ip, r.FormValue("prefix"), r.FormValue("gateway"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := a.state.SetAddress(parsed, prefix, gateway); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) stateEndpoint(w http.ResponseWriter, _ *http.Request) {
	_, _, cfg := a.state.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
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

func parseAddressForm(ip, prefix, gateway string) (string, int, string, error) {
	var parsed string
	bits := 24
	var err error
	if strings.Contains(ip, "/") {
		parsed, bits, err = parseIPv4CIDR(ip)
		if err != nil {
			return "", 0, "", err
		}
	} else {
		address := net.ParseIP(strings.TrimSpace(ip))
		if address == nil || address.To4() == nil {
			return "", 0, "", fmt.Errorf("expected IPv4 address")
		}
		parsed = address.To4().String()
	}
	p := bits
	if prefix != "" {
		p, err = strconv.Atoi(prefix)
		if err != nil {
			return "", 0, "", err
		}
	}
	return parsed, p, gateway, nil
}
