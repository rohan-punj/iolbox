package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

//go:embed templates/*.html static/*
var assets embed.FS

type packetHandler func(*net.UDPConn, []byte, *net.UDPAddr)

type udpBinding struct {
	mu      sync.RWMutex
	name    string
	conn    *net.UDPConn
	handler packetHandler
	err     string
}

func (b *udpBinding) rebind(port int) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		b.mu.Lock()
		b.err = err.Error()
		b.mu.Unlock()
		return err
	}
	if b.name == "DHCP" {
		if err := prepareDHCPConn(conn); err != nil {
			_ = conn.Close()
			b.mu.Lock()
			b.err = err.Error()
			b.mu.Unlock()
			return err
		}
	}
	b.mu.Lock()
	old := b.conn
	b.conn = conn
	b.err = ""
	b.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go b.serve(conn)
	return nil
}

func (b *udpBinding) serve(conn *net.UDPConn) {
	buf := make([]byte, 65535)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		packet := append([]byte(nil), buf[:n]...)
		go b.handler(conn, packet, addr)
	}
}

func (b *udpBinding) close() {
	b.mu.Lock()
	conn := b.conn
	b.conn = nil
	b.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (b *udpBinding) error() string { b.mu.RLock(); defer b.mu.RUnlock(); return b.err }

type App struct {
	store       *Store
	optionsPath string
	dhcp        *DHCPServer
	dns         *DNSServer
	ntp         *NTPServer
	tftp        *TFTPServer
	bindings    map[string]*udpBinding
}

func NewApp(store *Store, optionsPath string) *App {
	a := &App{store: store, optionsPath: optionsPath, bindings: map[string]*udpBinding{}}
	a.dhcp = NewDHCPServer(store)
	a.dns = NewDNSServer(store)
	a.ntp = NewNTPServer(store)
	a.tftp = NewTFTPServer(store, optionsPath)
	a.bindings["dhcp"] = &udpBinding{name: "DHCP", handler: a.dhcp.handlePacket}
	a.bindings["dns"] = &udpBinding{name: "DNS", handler: a.dns.handlePacket}
	a.bindings["ntp"] = &udpBinding{name: "NTP", handler: a.ntp.handlePacket}
	a.bindings["tftp"] = &udpBinding{name: "TFTP", handler: a.tftp.handlePacket}
	return a
}

func (a *App) StartServices() {
	cfg := a.store.Snapshot()
	ports := map[string]int{"dns": cfg.Ports.DNS, "dhcp": cfg.Ports.DHCP, "ntp": cfg.Ports.NTP, "tftp": cfg.Ports.TFTP}
	for name, port := range ports {
		if err := a.bindings[name].rebind(port); err != nil {
			log.Printf("netsvc: %s bind UDP %d failed: %v", name, port, err)
		}
	}
}

// Rebind applies a settings change only after a replacement socket is ready.
// A failed replacement leaves the old listener serving, as required by the
// pack contract, while the dashboard exposes the failed requested port.
func (a *App) Rebind(cfg Config) {
	ports := map[string]int{"dns": cfg.Ports.DNS, "dhcp": cfg.Ports.DHCP, "ntp": cfg.Ports.NTP, "tftp": cfg.Ports.TFTP}
	for name, port := range ports {
		if err := a.bindings[name].rebind(port); err != nil {
			log.Printf("netsvc: %s rebind UDP %d failed: %v", name, port, err)
		}
	}
}

func (a *App) Close() {
	for _, binding := range a.bindings {
		binding.close()
	}
}

type dashboardData struct {
	Config               Config
	Iface                bool
	DNS, DHCP, NTP, TFTP string
	Leases               []Lease
	Queries              []DNSLog
	NTPRows              []NTPLog
	Transfers            []TFTPLog
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	data := dashboardData{
		Config: a.store.Snapshot(), Iface: hasLabIface(),
		DNS: a.bindings["dns"].error(), DHCP: a.bindings["dhcp"].error(),
		NTP: a.bindings["ntp"].error(), TFTP: a.bindings["tftp"].error(),
		Leases: a.dhcp.Leases(), Queries: a.dns.Logs(), NTPRows: a.ntp.Logs(), Transfers: a.tftp.Logs(),
	}
	render(w, "dashboard.html", data)
}

func (a *App) fragment(name string, w http.ResponseWriter) {
	data := dashboardData{Config: a.store.Snapshot(), Leases: a.dhcp.Leases(), Queries: a.dns.Logs(), NTPRows: a.ntp.Logs(), Transfers: a.tftp.Logs()}
	renderFragment(w, name, data)
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Snapshot()
	records, _ := json.MarshalIndent(cfg.DNS.Records, "", "  ")
	render(w, "settings.html", struct {
		Config  Config
		Records string
	}{cfg, string(records)})
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := a.store.Snapshot()
	cfg.DHCP.ServerIP = strings.TrimSpace(r.FormValue("server_ip"))
	cfg.DHCP.DNSServers = splitCSV(r.FormValue("dns_servers"))
	cfg.DHCP.NTPServers = splitCSV(r.FormValue("ntp_servers"))
	cfg.DHCP.TFTPName = strings.TrimSpace(r.FormValue("tftp_name"))
	cfg.DHCP.TFTPAddresses = splitCSV(r.FormValue("tftp_addresses"))
	cfg.DNS.Zone = strings.TrimSpace(r.FormValue("zone"))
	if text := strings.TrimSpace(r.FormValue("records_json")); text != "" {
		var records []DNSRecord
		if err := json.Unmarshal([]byte(text), &records); err != nil {
			http.Error(w, "invalid records JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		cfg.DNS.Records = records
	}
	if pool := cfg.DHCP.Pools; len(pool) > 0 {
		pool[0].Subnet = strings.TrimSpace(r.FormValue("pool_subnet"))
		pool[0].RangeStart = strings.TrimSpace(r.FormValue("pool_start"))
		pool[0].RangeEnd = strings.TrimSpace(r.FormValue("pool_end"))
		pool[0].Router = strings.TrimSpace(r.FormValue("pool_router"))
		cfg.DHCP.Pools = pool
	}
	cfg.NTP.ServerIP = strings.TrimSpace(r.FormValue("ntp_server_ip"))
	if n, err := strconv.Atoi(r.FormValue("ntp_stratum")); err == nil && n >= 1 && n <= 16 {
		cfg.NTP.Stratum = uint8(n)
	}
	for key, target := range map[string]*int{"dns_port": &cfg.Ports.DNS, "dhcp_port": &cfg.Ports.DHCP, "ntp_port": &cfg.Ports.NTP, "tftp_port": &cfg.Ports.TFTP} {
		if n, err := strconv.Atoi(r.FormValue(key)); err == nil && n > 0 && n <= 65535 {
			*target = n
		}
	}
	a.Rebind(cfg)
	if err := a.store.Save(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
	mux.HandleFunc("GET /frag/dhcp", func(w http.ResponseWriter, r *http.Request) { a.fragment("dhcp.html", w) })
	mux.HandleFunc("GET /frag/dns", func(w http.ResponseWriter, r *http.Request) { a.fragment("dns.html", w) })
	mux.HandleFunc("GET /frag/ntp", func(w http.ResponseWriter, r *http.Request) { a.fragment("ntp.html", w) })
	mux.HandleFunc("GET /frag/tftp", func(w http.ResponseWriter, r *http.Request) { a.fragment("tftp.html", w) })
	return mux
}

func render(w http.ResponseWriter, page string, data any) {
	t, err := template.ParseFS(assets, "templates/layout.html", "templates/rows.html", "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func renderFragment(w http.ResponseWriter, page string, data any) {
	t, err := template.ParseFS(assets, "templates/rows.html", "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "fragment", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
