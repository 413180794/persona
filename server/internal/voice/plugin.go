package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Plugin serves the voice plugin's HTTP API on the hub's socket.
type Plugin struct {
	cfg      Config
	cat      *catalog
	hub      *hubClient
	logger   *log.Logger
	delegate delegateOptions
	active   atomic.Int32 // open conversations: a redeploy waits for 0 (GET /health)
}

func New(cfg Config, hubURL, hubToken string, logger *log.Logger) *Plugin {
	c := cfg
	return &Plugin{cfg: c, cat: &catalog{cfg: &c, now: time.Now}, hub: newHubClient(hubURL, hubToken), logger: logger, delegate: defaultDelegate}
}

func (pl *Plugin) logf(format string, args ...any) { pl.logger.Printf(format, args...) }

// Handler is the plugin's API (docs/plugins.md「语音」): /health, /models, /voices, /ws.
func (pl *Plugin) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, map[string]any{"ok": true, "conversations": pl.active.Load()})
	})
	mux.HandleFunc("GET /models", func(w http.ResponseWriter, r *http.Request) {
		models, def, err := pl.cat.models(r.Context(), pl.logf)
		if err != nil {
			writeHTTPJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"models": models, "default": def})
	})
	mux.HandleFunc("GET /voices", func(w http.ResponseWriter, r *http.Request) {
		model := r.URL.Query().Get("model")
		if model != "" {
			if _, _, ok := splitModel(model); !ok {
				writeHTTPJSON(w, http.StatusBadRequest, map[string]any{"error": "bad model id"})
				return
			}
		}
		v, err := pl.cat.voices(r.Context(), model)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, errNoProvider) {
				status = http.StatusNotFound
			}
			writeHTTPJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeHTTPJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		// Only the hub reaches this socket (0600), and it has checked the app.
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		c.SetReadLimit(1 << 20)
		pl.logf("conversation opened")
		pl.converse(context.WithoutCancel(r.Context()), c)
	})
	return mux
}

func writeHTTPJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Run is `jarvisd voice`: serve on JARVIS_PLUGIN_SOCKET until ctx ends.
func Run(ctx context.Context) error {
	logger := log.New(os.Stderr, "", 0)
	socket := os.Getenv("JARVIS_PLUGIN_SOCKET")
	if socket == "" {
		return errors.New("JARVIS_PLUGIN_SOCKET is not set: this runs under the Jarvis hub (plugins in ~/.jarvis/hub.json, docs/plugins.md)")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	hubURL := os.Getenv("JARVIS_HUB_URL")
	if hubURL == "" {
		hubURL = "http://127.0.0.1:18660"
	}
	pl := New(cfg, hubURL, os.Getenv("JARVIS_HUB_TOKEN"), logger)
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}
	for _, p := range cfg.Providers {
		via := ""
		if p.SSH != "" {
			via = " via " + p.SSH
		}
		logger.Printf("provider %s: %s dialect%s", p.ID, p.Dialect, via)
	}
	logger.Printf("listening on %s", socket)
	srv := &http.Server{Handler: pl.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
