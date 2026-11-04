package assistant

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/identity"
)

// Main is `personad assistant`: the assistant as its own process, on the hub's bus. The hub starts one beside itself
// (hub/assistant_process.go) and hands it its token on stdin; one run elsewhere (hub.json assistant.external) gets it
// from PERSONA_ASSISTANT_TOKEN (`personad hub assistant-token` makes one). It keeps its state in the hub's Postgres,
// found as the hub finds it: hub.json's database_url, PERSONA_DATABASE_URL over it.
func Main(ctx context.Context, version string, stdin io.Reader) error {
	logger := log.New(os.Stderr, "assistant: ", log.LstdFlags)
	cfg, err := readHubConfig()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("no database_url in hub.json (or PERSONA_DATABASE_URL)")
	}
	token := strings.TrimSpace(identity.Getenv("JARVIS_ASSISTANT_TOKEN"))
	if token == "" && stdin != nil {
		r := bufio.NewReader(stdin)
		line, _ := r.ReadString('\n')
		token = strings.TrimSpace(line)
		// Handed the token on stdin (by the hub that started it): it stops when stdin closes, the hub gone.
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			_, _ = io.Copy(io.Discard, r)
			logger.Printf("stdin closed (the hub that started it is gone): stopping")
			cancel()
		}()
	}
	if token == "" {
		return errors.New("no bus token: set PERSONA_ASSISTANT_TOKEN (personad hub assistant-token prints one)")
	}
	hub := strings.TrimSpace(identity.Getenv("JARVIS_ASSISTANT_HUB"))
	if hub == "" {
		hub = localHubURL(cfg.Listen)
	}
	deploy := identity.Getenv("JARVIS_ASSISTANT_DEPLOY")
	if deploy == "" {
		deploy = "external"
	}
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()
	bh := NewBusHub(BusOptions{Hub: hub, Token: token, Version: version, Deploy: deploy, Logf: logger.Printf})
	svc := New(Options{Hub: bh, Store: func() Store { return database }, Logf: logger.Printf})
	logger.Printf("connecting to %s", hub)
	if err := bh.Run(ctx, svc); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// hubConfig is what the assistant reads of the hub's hub.json.
type hubConfig struct {
	DatabaseURL string `json:"database_url"`
	Listen      string `json:"listen"`
}

// readHubConfig reads hub.json in the data directory (~/.persona, or ~/.jarvis) with the environment over it, as the
// hub does (its envOverrides); a missing file is no error.
func readHubConfig() (hubConfig, error) {
	cfg := hubConfig{Listen: ":18660"}
	b, err := os.ReadFile(filepath.Join(identity.HomeDir(), "hub.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("hub.json: %w", err)
		}
	case !os.IsNotExist(err):
		return cfg, err
	}
	if v := identity.Getenv("JARVIS_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := identity.Getenv("JARVIS_LISTEN"); v != "" {
		cfg.Listen = v
	}
	return cfg, nil
}

// localHubURL is the hub on this machine, from its listen address.
func localHubURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:18660"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
