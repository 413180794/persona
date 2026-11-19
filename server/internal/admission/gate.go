// Package admission provides an opt-in, site-local admission barrier shared
// by the hub and its manager. "drained" covers only registered operations.
package admission

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const Env = "PERSONA_ADMISSION_DIR"

var (
	ErrClosed   = errors.New("admission is closed")
	idPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,80}$`)
	kindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
)

type Gate struct{ dir string }

type State struct {
	Phase   string         `json:"phase"`
	Attempt string         `json:"attempt,omitempty"`
	Active  map[string]int `json:"active,omitempty"`
}

// FromEnv returns a nil, open gate for sites that have not configured admission.
func FromEnv() (*Gate, error) {
	dir := os.Getenv(Env)
	if dir == "" {
		return nil, nil
	}
	return New(dir)
}

// New requires a pre-provisioned, private local directory that outlives the
// process and database slots. It must be shared with the built-in manager.
func New(dir string) (*Gate, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, fmt.Errorf("%s must be a clean absolute path", Env)
	}
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	return &Gate{dir: dir}, nil
}

type handle struct{ f *os.File }

func (g *Gate) lock() (handle, error) {
	if err := checkDir(g.dir); err != nil {
		return handle{}, err
	}
	return lockDir(g.dir)
}

func (h handle) close() { _ = h.f.Close() }

func (g *Gate) state() (State, error) {
	var s State
	raw, err := os.ReadFile(filepath.Join(g.dir, "state.json"))
	if err != nil {
		return s, fmt.Errorf("read persistent admission state: %w", err)
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	switch s.Phase {
	case "open":
		if s.Attempt != "" {
			return s, errors.New("invalid open admission state")
		}
	case "draining", "drained":
		if !idPattern.MatchString(s.Attempt) {
			return s, errors.New("invalid closed admission attempt")
		}
	default:
		return s, errors.New("invalid admission phase")
	}
	if len(s.Active) != 0 {
		return s, errors.New("invalid admission state with embedded operation counts")
	}
	return s, nil
}

func (g *Gate) active() (map[string]int, error) {
	files, err := os.ReadDir(g.dir)
	if err != nil {
		return nil, err
	}
	active := make(map[string]int)
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "work-") {
			continue
		}
		kind, _, found := strings.Cut(strings.TrimPrefix(file.Name(), "work-"), ".")
		if !found || !kindPattern.MatchString(kind) || !file.Type().IsRegular() {
			return nil, fmt.Errorf("invalid admission operation %q", file.Name())
		}
		active[kind]++
	}
	return active, nil
}

func (g *Gate) sync() error {
	f, err := os.Open(g.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (g *Gate) publish(s State) error {
	tmp, err := os.CreateTemp(g.dir, ".state.")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = json.NewEncoder(tmp).Encode(s)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(g.dir, "state.json")); err != nil {
		return err
	}
	return g.sync()
}

// Admit durably records an operation before its caller can acknowledge it.
// An unclean process exit leaves its record behind, so wait cannot mistake
// process disappearance for successful completion.
func (g *Gate) Admit(kind string) (func(), error) {
	return g.record(kind, "", "", nil)
}

// AdmitPayload retains enough input to reconcile work after process loss.
func (g *Gate) AdmitPayload(kind, id string, payload []byte) (func(), error) {
	return g.record(kind, "", id, payload)
}

// ContinuePayload records a handoff even after admission closes, but only while
// the same input still has its parent operation. It cannot reopen a fence.
func (g *Gate) ContinuePayload(parent, kind, id string, payload []byte) (func(), error) {
	return g.record(kind, parent, id, payload)
}

func (g *Gate) record(kind, parent, id string, payload []byte) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	if !kindPattern.MatchString(kind) {
		return nil, errors.New("invalid operation kind")
	}
	lock, err := g.lock()
	if err != nil {
		return nil, err
	}
	defer lock.close()
	s, err := g.state()
	if err != nil {
		return nil, err
	}
	prefix := "work-" + kind + "."
	if id != "" {
		prefix += fmt.Sprintf("%x.", sha256.Sum256([]byte(id)))
	}
	if s.Phase != "open" {
		if s.Phase != "draining" || parent == "" || id == "" || !kindPattern.MatchString(parent) {
			return nil, ErrClosed
		}
		parentPrefix := fmt.Sprintf("work-%s.%x.", parent, sha256.Sum256([]byte(id)))
		files, err := os.ReadDir(g.dir)
		if err != nil {
			return nil, err
		}
		found := false
		for _, file := range files {
			if file.Type().IsRegular() && strings.HasPrefix(file.Name(), parentPrefix) {
				found = true
				break
			}
		}
		if !found {
			return nil, ErrClosed
		}
	}
	op, err := os.CreateTemp(g.dir, prefix)
	if err != nil {
		return nil, err
	}
	_, writeErr := op.Write(payload)
	syncErr := op.Sync()
	closeErr := op.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, err
	}
	if err := g.sync(); err != nil {
		return nil, err
	}
	return func() {
		if err := g.finish(op.Name()); err != nil {
			log.Printf("admission left an unsettled %s operation: %v", kind, err)
		}
	}, nil
}

func (g *Gate) finish(path string) error {
	lock, err := g.lock()
	if err != nil {
		return err
	}
	defer lock.close()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return g.sync()
}

// Control is used only by the local operator. Closing is idempotent for the
// same attempt; another attempt cannot reopen or claim this fence.
func (g *Gate) Control(action, attempt string) (State, error) {
	if action != "status" && action != "init" && !idPattern.MatchString(attempt) {
		return State{}, errors.New("invalid admission attempt")
	}
	lock, err := g.lock()
	if err != nil {
		return State{}, err
	}
	defer lock.close()
	s, err := g.state()
	if action == "init" && errors.Is(err, os.ErrNotExist) {
		active, err := g.active()
		if err != nil {
			return State{}, err
		}
		if len(active) != 0 {
			return State{}, errors.New("refusing to initialize admission with unsettled operations")
		}
		s = State{Phase: "open"}
		return s, g.publish(s)
	}
	if err != nil {
		return s, err
	}
	if action != "status" && s.Attempt != attempt && (action != "close" || s.Phase != "open") {
		return s, errors.New("admission attempt does not own the fence")
	}
	active, err := g.active()
	if err != nil {
		return s, err
	}
	previous := s
	switch action {
	case "status":
	case "close":
		if s.Phase == "open" {
			s = State{Phase: "draining", Attempt: attempt}
		}
	case "check":
		if len(active) == 0 {
			s.Phase = "drained"
		}
	case "open":
		if len(active) != 0 {
			return s, errors.New("admission has unsettled operations")
		}
		s = State{Phase: "open"}
	default:
		return s, errors.New("unknown admission action")
	}
	if s.Phase != previous.Phase {
		err = g.publish(s)
	}
	s.Active = active
	return s, err
}

// Startup guards bootstrap writes, including migrations. A process restarted
// while admission is closed waits without opening its database.
func (g *Gate) Startup(ctx context.Context, kind string) (func(), error) {
	for {
		done, err := g.Admit(kind)
		if !errors.Is(err, ErrClosed) {
			return done, err
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}
