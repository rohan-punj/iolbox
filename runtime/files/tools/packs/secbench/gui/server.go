package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

type logRow struct {
	Msg string
	Cls string
}

// ModuleView bundles a ModuleDef with its live runtime state + saved params
// for the template — the one generic "module-card" partial renders any of
// the 26 modules from this.
type ModuleView struct {
	Def         ModuleDef
	Running     bool
	StartedAt   string
	Log         []logRow
	Params      map[string]string
	RawArgs     string
	HasLabIface bool
}

type pageData struct {
	Page         string
	Title        string
	Cfg          Config
	IFaces       []ifaceInfo
	HasLabIface  bool
	RunningCount int
	Group        string
	GroupLabel   string
	GroupDesc    string
	Modules      []ModuleView
	ReconHosts   []ReconHost
	Blob         string
	RawKeys      []string
	RawSel       string
	RawText      string
}

func (a *App) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("GET /{$}", a.dashboard)
	mux.HandleFunc("GET /group/{group}", a.groupPage)
	mux.HandleFunc("GET /raw", a.rawPage)
	mux.HandleFunc("GET /settings", a.settingsPage)
	mux.HandleFunc("GET /frag/dash", a.fragDash)
	mux.HandleFunc("GET /frag/module/{key}", a.fragModule)
	mux.HandleFunc("POST /module/{key}/start", a.moduleStart)
	mux.HandleFunc("POST /module/{key}/stop", a.moduleStop)
	mux.HandleFunc("POST /stopall", a.stopAll)
	mux.HandleFunc("POST /raw/save", a.rawSave)
}

func (a *App) render(w http.ResponseWriter, name string, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, name, d); err != nil {
		logf("template %s: %v", name, err)
		http.Error(w, "template error", 500)
	}
}

// ---- view building ----

func (a *App) moduleView(m ModuleDef) ModuleView {
	cfg := a.store.Get()
	r := a.sup.get(m.Key)
	lines := r.Tail(200)
	rows := make([]logRow, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, logRow{Msg: l, Cls: classify(l)})
	}
	params := cfg.ModuleParams[m.Key]
	if params == nil {
		params = map[string]string{}
	}
	started := ""
	if r.IsRunning() {
		started = r.StartedAt().Format("15:04:05")
	}
	return ModuleView{
		Def: m, Running: r.IsRunning(), StartedAt: started, Log: rows,
		Params: params, RawArgs: cfg.RawArgs[m.Key], HasLabIface: hasLabIface(),
	}
}

func (a *App) allModuleViews() []ModuleView {
	out := make([]ModuleView, 0, len(moduleDefs))
	for _, m := range moduleDefs {
		out = append(out, a.moduleView(m))
	}
	return out
}

// ---- pages ----

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	views := a.allModuleViews()
	var hosts []ReconHost
	if rr := a.sup.get("arp_scan"); rr != nil {
		hosts = parseReconHosts(rr.Tail(500))
	}
	a.render(w, "page-dashboard", pageData{
		Page: "dashboard", Title: "Dashboard", Cfg: a.store.Get(),
		IFaces: listIfaces(), HasLabIface: hasLabIface(), RunningCount: a.sup.RunningCount(),
		Modules: views, ReconHosts: hosts,
	})
}

func (a *App) fragDash(w http.ResponseWriter, r *http.Request) {
	a.render(w, "frag-dash", pageData{
		IFaces: listIfaces(), HasLabIface: hasLabIface(), RunningCount: a.sup.RunningCount(),
		Modules: a.allModuleViews(),
	})
}

var groupMeta = func() map[string]struct{ Label, Desc string } {
	m := map[string]struct{ Label, Desc string }{}
	for _, g := range groupOrder {
		m[g.Key] = struct{ Label, Desc string }{g.Label, g.Desc}
	}
	return m
}()

func (a *App) groupPage(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	meta, ok := groupMeta[group]
	if !ok {
		http.NotFound(w, r)
		return
	}
	mods := modulesInGroup(group)
	views := make([]ModuleView, 0, len(mods))
	for _, m := range mods {
		views = append(views, a.moduleView(m))
	}
	var hosts []ReconHost
	if group == groupRecon || group == groupSpoof || group == groupDHCP {
		if rr := a.sup.get("arp_scan"); rr != nil {
			hosts = parseReconHosts(rr.Tail(500))
		}
	}
	a.render(w, "page-group", pageData{
		Page: group, Title: meta.Label, Group: group, GroupLabel: meta.Label, GroupDesc: meta.Desc,
		Cfg: a.store.Get(), Modules: views, ReconHosts: hosts, HasLabIface: hasLabIface(),
	})
}

func (a *App) fragModule(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	m := moduleByKey(key)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, "frag-module-live", pageData{Modules: []ModuleView{a.moduleView(*m)}})
}

func (a *App) rawPage(w http.ResponseWriter, r *http.Request) {
	keys := make([]string, 0, len(moduleDefs))
	for _, m := range moduleDefs {
		keys = append(keys, m.Key)
	}
	sort.Strings(keys)
	sel := r.URL.Query().Get("m")
	if sel == "" {
		sel = keys[0]
	}
	cfg := a.store.Get()
	a.render(w, "page-raw", pageData{Page: "raw", Title: "Raw", Cfg: cfg, RawKeys: keys, RawSel: sel, RawText: cfg.RawArgs[sel]})
}

func (a *App) rawSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sel := r.FormValue("m")
	text := strings.TrimSpace(r.FormValue("content"))
	_ = a.store.Update(func(c *Config) {
		if c.RawArgs == nil {
			c.RawArgs = map[string]string{}
		}
		c.RawArgs[sel] = text
	})
	http.Redirect(w, r, "/raw?m="+sel, http.StatusSeeOther)
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Get()
	blob, _ := json.MarshalIndent(cfg, "", "  ")
	a.render(w, "page-settings", pageData{Page: "settings", Title: "Settings", Cfg: cfg, Blob: string(blob)})
}

func (a *App) moduleStart(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	m := moduleByKey(key)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()

	params := map[string]string{}
	var extra []string
	for _, f := range m.Fields {
		v := strings.TrimSpace(r.FormValue(f.Name))
		params[f.Name] = v
		if v != "" {
			extra = append(extra, "--"+f.Name, v)
		}
	}
	count := strings.TrimSpace(r.FormValue("count"))
	if count == "" {
		count = "0"
	}
	interval := strings.TrimSpace(r.FormValue("interval"))
	if interval == "" {
		interval = "1"
	}
	extra = append(extra, "--count", count, "--interval", interval)

	cfg := a.store.Get()
	if raw := strings.TrimSpace(cfg.RawArgs[key]); raw != "" {
		extra = append(extra, strings.Fields(raw)...)
	}

	_ = a.store.Update(func(c *Config) {
		if c.ModuleParams == nil {
			c.ModuleParams = map[string]map[string]string{}
		}
		params["count"] = count
		params["interval"] = interval
		c.ModuleParams[key] = params
	})

	if err := a.sup.Start(key, extra); err != nil {
		logf("start %s: %v", key, err)
		if r := a.sup.get(key); r != nil {
			r.Note(err.Error())
		}
	}
	a.render(w, "frag-module-live", pageData{Modules: []ModuleView{a.moduleView(*m)}})
}

func (a *App) moduleStop(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	m := moduleByKey(key)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	a.sup.Stop(key)
	a.render(w, "frag-module-live", pageData{Modules: []ModuleView{a.moduleView(*m)}})
}

func (a *App) stopAll(w http.ResponseWriter, r *http.Request) {
	a.sup.StopAll()
	a.render(w, "frag-dash", pageData{
		IFaces: listIfaces(), HasLabIface: hasLabIface(), RunningCount: a.sup.RunningCount(),
		Modules: a.allModuleViews(),
	})
}
