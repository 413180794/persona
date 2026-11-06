package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/update"
)

// A manager made in the app (docs/managers.md, "A second manager") is the same program as the assistant the hub starts, with
// a directory of its own: manager.json there says who it is and where the hub is, and everything it keeps on disk (the
// knowledge base, secret.key, its extensions) is under it, never under the hub's. Its state is in a Postgres of its own,
// never the hub's: the container `personad manager install` makes for it (internal/manager), or the one the user gave.

// ManagerFile is a manager's configuration, in its directory (0600: it holds the manager's bus token).
const ManagerFile = "manager.json"

// ManagerConfig is manager.json.
type ManagerConfig struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Hub is the hub's base URL as this manager reaches it (http(s)://host:port), HubID the hub it joined.
	Hub   string `json:"hub"`
	HubID string `json:"hub_id,omitempty"`
	Token string `json:"token"`
	// Fingerprint pins the hub's TLS key on an https Hub with the hub's own certificate (link.Pin); "" trusts the
	// system's CAs.
	Fingerprint string `json:"fingerprint,omitempty"`
	// DatabaseURL is its own database: its container's (DatabaseContainer), or the one --database-url gave (made
	// when missing). Empty until `personad manager install` (or run) made its container.
	DatabaseURL string `json:"database_url,omitempty"`
	// DatabaseContainer is the Docker container its Postgres runs in when install made it (persona-mgr-<id>-pg); ""
	// for a database the user gave, which uninstall never touches.
	DatabaseContainer string `json:"database_container,omitempty"`
}

// managerID is the shape of a manager's id (the hub's busManagerID).
var managerID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (c *ManagerConfig) check() error {
	switch {
	case !managerID.MatchString(c.ID):
		return fmt.Errorf("manager id %q: lower-case letters, digits and -", c.ID)
	case c.ID == assistantwire.AssistantID:
		return errors.New("the hub runs the manager " + c.ID + " itself (personad assistant)")
	case !strings.HasPrefix(c.Hub, "http://") && !strings.HasPrefix(c.Hub, "https://"):
		return fmt.Errorf("hub %q: want http(s)://host:port", c.Hub)
	case strings.TrimSpace(c.Token) == "":
		return errors.New("no token")
	}
	return nil
}

// DefaultManagerDir is where `personad manager join` puts manager id when not told: managers/<id> in this machine's
// data directory (~/.persona/managers/<id>).
func DefaultManagerDir(id string) string { return filepath.Join(identity.HomeDir(), "managers", id) }

// ReadManagerConfig reads manager.json in dir.
func ReadManagerConfig(dir string) (*ManagerConfig, error) {
	b, err := os.ReadFile(filepath.Join(dir, ManagerFile))
	if err != nil {
		return nil, err
	}
	var c ManagerConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, ManagerFile), err)
	}
	if err := c.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, ManagerFile), err)
	}
	return &c, nil
}

// WriteManagerConfig writes manager.json in dir (made 0700 when missing), 0600, in one step: a manager running from
// it reads either the old one or the new one.
func WriteManagerConfig(dir string, c ManagerConfig) error {
	if err := c.check(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".manager-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, ManagerFile))
}

// HubDatabaseURL is the database_url of the hub on this machine (hub.json in the data directory,
// PERSONA_DATABASE_URL over it); "" when there is none. A manager never uses it (SameDatabase).
func HubDatabaseURL() string {
	cfg, err := readHubConfig()
	if err != nil {
		return ""
	}
	return cfg.DatabaseURL
}

// ErrHubDatabase: the database a manager was given is the hub's own.
var ErrHubDatabase = errors.New("this is the hub's own database: a manager keeps its state in a Postgres of its own")

// SameDatabase: a and b name the same database on the same server, as far as their URLs tell: host names compared
// without case, every loopback name (localhost, 127.0.0.0/8, ::1) as one, a missing port as 5432.
func SameDatabase(a, b string) bool {
	server := func(raw string) string {
		u, err := neturl.Parse(raw)
		if err != nil {
			return raw
		}
		host, port := strings.ToLower(u.Hostname()), u.Port()
		if port == "" {
			port = "5432"
		}
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			host = "loopback"
		}
		return host + ":" + port
	}
	return server(a) == server(b) && db.DatabaseName(a) == db.DatabaseName(b)
}

// RunManager is `personad manager run`: the manager whose manager.json is in dir, until ctx ends. Its data
// directory is dir (PERSONA_HOME too, for what it starts); its database is manager.json's database_url (made when it
// is missing on that server), which the caller made sure there is (manager.ownDatabase). When the hub refuses its
// token (removed in the app, or installed again elsewhere) it says so once and returns bus.ErrRevoked, not trying
// again with it.
func RunManager(ctx context.Context, build update.Build, dir string) error {
	logger := log.New(os.Stderr, "manager: ", log.LstdFlags)
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	c, err := ReadManagerConfig(dir)
	if err != nil {
		return err
	}
	url := c.DatabaseURL
	if url == "" {
		return errors.New("no database: run personad manager install, which makes its own Postgres in Docker, or give --database-url")
	}
	if created, err := db.EnsureDatabase(ctx, url); err != nil {
		return fmt.Errorf("database %s: %w", db.DatabaseName(url), err)
	} else if created {
		logger.Printf("[%s] made its database %s", c.ID, db.DatabaseName(url))
	}
	// What it starts (extensions, tools) finds its data directory, not the hub's.
	_ = os.Setenv("PERSONA_HOME", dir)
	_ = os.Setenv("JARVIS_HOME", dir)
	deploy := identity.Getenv("JARVIS_ASSISTANT_DEPLOY")
	if deploy == "" {
		deploy = "external"
		if _, err := os.Stat("/.dockerenv"); err == nil {
			deploy = "docker"
		}
	}
	return run(ctx, build, runOptions{ID: c.ID, Hub: c.Hub, Token: c.Token, Fingerprint: c.Fingerprint, Deploy: deploy,
		Dir: dir, DatabaseURL: url, StopWhenRevoked: true, Logf: logger.Printf})
}

// pinnedHTTPClient dials the hub only when its TLS key is fp's.
func pinnedHTTPClient(fp string) *http.Client { return link.Pinned(fp) }
