package hub

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// PluginConfig is a process the hub runs next to itself to add something to the app (docs/plugins.md). The hub
// starts it, restarts it when it exits and relays /api/v1/plugins/{id}/… to the HTTP server it serves on the unix
// socket in JARVIS_PLUGIN_SOCKET (WebSockets included). Without a command the plugin runs elsewhere and only its
// socket is here (e.g. forwarded with ssh -R from a machine that reaches what the plugin needs). What a plugin does lives in its own repository; the hub only
// knows its kind, which tells the app what to show (kind "voice": the voice conversation, docs/plugins.md).
type PluginConfig struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Kind     string            `json:"kind"`
	Command  string            `json:"command,omitempty"`
	Socket   string            `json:"socket,omitempty"` // default ~/.jarvis/plugins/<id>.sock
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Dir      string            `json:"dir,omitempty"` // working directory; "~/" is the hub machine's home
	Disabled bool              `json:"disabled,omitempty"`
}

// PluginView is a plugin as the app sees it (GET /api/v1/plugins).
type PluginView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"` // starting | running | error | disabled
	Error  string `json:"error,omitempty"`
}

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type plugin struct {
	cfg    PluginConfig
	socket string
	proxy  *httputil.ReverseProxy
	client *http.Client

	mu     sync.Mutex
	status string
	err    string
}

type plugins struct {
	byID map[string]*plugin
}

func newPlugins(dir string, cfgs []PluginConfig, logf func(string, ...any)) *plugins {
	ps := &plugins{byID: map[string]*plugin{}}
	for _, c := range cfgs {
		if !pluginIDPattern.MatchString(c.ID) || (c.Command == "" && c.Socket == "") {
			logf("plugin %q: skipped (needs an id of a-z0-9_- and a command or a socket)", c.ID)
			continue
		}
		if ps.byID[c.ID] != nil {
			logf("plugin %q: skipped (duplicate id)", c.ID)
			continue
		}
		if c.Name == "" {
			c.Name = c.ID
		}
		socket := filepath.Join(dir, "plugins", c.ID+".sock")
		if c.Socket != "" {
			socket = expandHome(c.Socket)
		}
		p := &plugin{cfg: c, socket: socket, status: "starting"}
		if c.Disabled {
			p.status = "disabled"
		}
		dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", p.socket)
		}
		p.client = &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: 5 * time.Second}
		prefix := "/api/v1/plugins/" + c.ID
		p.proxy = &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.Out.URL.Scheme, r.Out.URL.Host = "http", "plugin"
				r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, prefix)
				if r.Out.URL.Path == "" {
					r.Out.URL.Path = "/"
				}
				r.Out.URL.RawPath = ""
				// The hub has already checked the caller: the plugin never sees the app's credential.
				q := r.Out.URL.Query()
				q.Del("token")
				r.Out.URL.RawQuery = q.Encode()
				r.Out.Header.Del("Authorization")
				r.Out.Host = "plugin"
			},
			Transport:     &http.Transport{DialContext: dial},
			FlushInterval: -1, // stream (audio, server-sent events) as it comes
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				writeErr(w, http.StatusBadGateway, fmt.Errorf("plugin %s is not answering: %w", c.ID, err))
			},
		}
		ps.byID[c.ID] = p
	}
	return ps
}

func (ps *plugins) list() []PluginView {
	out := []PluginView{}
	for _, p := range ps.byID {
		p.mu.Lock()
		out = append(out, PluginView{ID: p.cfg.ID, Name: p.cfg.Name, Kind: p.cfg.Kind, Status: p.status, Error: p.err})
		p.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (p *plugin) set(status, err string) {
	p.mu.Lock()
	p.status, p.err = status, err
	p.mu.Unlock()
}

// run keeps the plugin's process alive with the same backoff as a machine's node, and its status current.
func (p *plugin) run(ctx context.Context, env []string, logf func(string, ...any)) {
	if p.cfg.Disabled {
		return
	}
	if p.cfg.Command == "" {
		p.watch(ctx)
		return
	}
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := p.once(ctx, env, logf)
		if ctx.Err() != nil {
			return
		}
		msg := "exited"
		if err != nil {
			msg = err.Error()
		}
		logf("plugin %s: %s", p.cfg.ID, msg)
		p.set("error", msg)
		if time.Since(start) > time.Minute {
			backoff = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 60*time.Second)
	}
}

func (p *plugin) once(ctx context.Context, env []string, logf func(string, ...any)) error {
	if err := os.MkdirAll(filepath.Dir(p.socket), 0o700); err != nil {
		return err
	}
	_ = os.Remove(p.socket) // left by a previous run that was killed
	cmd := exec.Command(expandHome(p.cfg.Command), p.cfg.Args...)
	cmd.Dir = expandHome(p.cfg.Dir)
	cmd.Env = append(append(os.Environ(), env...), "JARVIS_PLUGIN_ID="+p.cfg.ID, "JARVIS_PLUGIN_SOCKET="+p.socket)
	for k, v := range p.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // so stopping it takes its children too
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	p.set("starting", "")
	if err := cmd.Start(); err != nil {
		return err
	}
	go logLines(out, func(line string) { logf("plugin %s: %s", p.cfg.ID, line) })
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			if err == nil {
				err = errors.New("exited")
			}
			return err
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return ctx.Err()
		case <-tick.C:
			// Up once its /health answers; checked every second until then, every 10 after.
			if p.healthy(ctx) {
				p.set("running", "")
				tick.Reset(10 * time.Second)
			} else {
				p.mu.Lock()
				if p.status == "running" {
					p.status, p.err = "error", "health check failed"
				}
				p.mu.Unlock()
			}
		}
	}
}

// watch keeps the status of a plugin the hub does not run: running while its socket answers /health.
func (p *plugin) watch(ctx context.Context) {
	for {
		if p.healthy(ctx) {
			p.set("running", "")
		} else {
			p.set("error", "not answering on "+p.socket)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (p *plugin) healthy(ctx context.Context) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://plugin/health", nil)
	res, err := p.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func logLines(r io.Reader, f func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			f(line)
		}
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// hubLocalURL is how a plugin on this machine reaches the hub's API.
func hubLocalURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:18660"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (h *Hub) startPlugins(ctx context.Context) {
	if h.plugins == nil {
		return
	}
	// A plugin is configured by whoever runs the hub, so it gets the hub's own credential to call its API with
	// (the knowledge base, the agents, …): like a node, it is part of the hub.
	env := []string{"JARVIS_HUB_URL=" + hubLocalURL(h.cfg.Listen), "JARVIS_HUB_TOKEN=" + h.cfg.Token}
	for _, p := range h.plugins.byID {
		go p.run(ctx, env, h.logf)
	}
}

func (h *Hub) pluginRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/plugins", func(w http.ResponseWriter, r *http.Request) {
		if h.plugins == nil {
			writeJSON(w, map[string]any{"plugins": []PluginView{}})
			return
		}
		writeJSON(w, map[string]any{"plugins": h.plugins.list()})
	})
	api.HandleFunc("/api/v1/plugins/{id}/", func(w http.ResponseWriter, r *http.Request) {
		// A plugin can do anything the hub can (it holds its token): not for an answer-only device.
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		var p *plugin
		if h.plugins != nil {
			p = h.plugins.byID[r.PathValue("id")]
		}
		if p == nil || p.cfg.Disabled {
			writeErr(w, http.StatusNotFound, errors.New("no such plugin"))
			return
		}
		p.proxy.ServeHTTP(w, r)
	})
}
