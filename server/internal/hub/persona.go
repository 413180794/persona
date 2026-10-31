package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Personas (docs/persona.md): a look for Jarvis — its name, face, the animated dot in the Jarvis tab's top bar and the
// colors — as a package in ~/.jarvis/personas/<id>/: persona.json and the files it names. They live on the hub and
// not in the app because a look may be someone else's character: the phone pulls the one it shows and caches it.
// The hub only lists and serves them. A package that does not parse, names a file it does not have or is too big is
// left out of the list (and logged), so the phone never gets half a look.

const (
	personaFormat = 1
	// The built-in look (drawn by the app) owns this id.
	personaBuiltin = "jarvis"
	// A package is a few hundred KB of PNGs; the bounds keep a mistake (a 4K source image left in) off the phones.
	personaMaxFile  = 4 << 20
	personaMaxTotal = 16 << 20
)

var personaIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// personaStates are the dot's states; idle is the one a package must have (the app shows it for any it lacks).
var personaStates = []string{"idle", "thinking", "working", "needs_you", "offline"}

// personaManifest is what the hub checks in persona.json; the app reads the rest (colors, frame rates).
type personaManifest struct {
	Format int    `json:"format"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar struct {
		Sizes map[string]string `json:"sizes"`
	} `json:"avatar"`
	Dot struct {
		States map[string]struct {
			File string `json:"file"`
		} `json:"states"`
	} `json:"dot"`
}

// personaInfo is one entry of GET /api/v1/personas: the manifest as written, and a rev that changes with any byte of
// the package (the phone keeps a copy per rev).
type personaInfo struct {
	ID       string          `json:"id"`
	Rev      string          `json:"rev"`
	Bytes    int64           `json:"bytes"`
	Manifest json.RawMessage `json:"manifest"`
}

func (h *Hub) personasDir() string { return filepath.Join(h.cfg.dir, "personas") }

// personas lists the packages that check out, by id.
func (h *Hub) personas() []personaInfo {
	entries, err := os.ReadDir(h.personasDir())
	if err != nil {
		return []personaInfo{}
	}
	out := []personaInfo{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p, err := loadPersona(h.personasDir(), e.Name())
		if err != nil {
			h.logf("persona %s left out: %v", e.Name(), err)
			continue
		}
		out = append(out, p)
	}
	return out
}

// loadPersona checks the package in dir/id and sums its files into a rev.
func loadPersona(dir, id string) (personaInfo, error) {
	if !personaIDPattern.MatchString(id) {
		return personaInfo{}, errors.New("the directory's name is not an id (a-z, 0-9, -)")
	}
	if id == personaBuiltin {
		return personaInfo{}, fmt.Errorf("%q is the built-in look's id", personaBuiltin)
	}
	root := filepath.Join(dir, id)
	raw, err := os.ReadFile(filepath.Join(root, "persona.json"))
	if err != nil {
		return personaInfo{}, err
	}
	var m personaManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return personaInfo{}, fmt.Errorf("persona.json: %w", err)
	}
	switch {
	case m.Format != personaFormat:
		return personaInfo{}, fmt.Errorf("format %d, this hub reads %d", m.Format, personaFormat)
	case m.ID != id:
		return personaInfo{}, fmt.Errorf("id %q is not the directory's name", m.ID)
	case strings.TrimSpace(m.Name) == "":
		return personaInfo{}, errors.New("no name")
	case len(m.Avatar.Sizes) == 0:
		return personaInfo{}, errors.New("no avatar")
	}
	if _, ok := m.Dot.States["idle"]; !ok {
		return personaInfo{}, errors.New("no idle dot")
	}
	named := []string{}
	for _, f := range m.Avatar.Sizes {
		named = append(named, f)
	}
	for s, st := range m.Dot.States {
		if !isPersonaState(s) {
			return personaInfo{}, fmt.Errorf("unknown dot state %q", s)
		}
		named = append(named, st.File)
	}
	for _, f := range named {
		if _, err := personaFile(root, f); err != nil {
			return personaInfo{}, err
		}
	}
	files, total, err := personaFiles(root)
	if err != nil {
		return personaInfo{}, err
	}
	if total > personaMaxTotal {
		return personaInfo{}, fmt.Errorf("%d bytes, more than %d", total, personaMaxTotal)
	}
	sum := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return personaInfo{}, err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", f, len(b))
		sum.Write(b)
	}
	return personaInfo{ID: id, Rev: hex.EncodeToString(sum.Sum(nil))[:16], Bytes: total, Manifest: raw}, nil
}

func isPersonaState(s string) bool {
	for _, x := range personaStates {
		if s == x {
			return true
		}
	}
	return false
}

// personaFile is a file of the package by its name in the manifest: a relative path inside the package, no dot files,
// a regular file (not a link out of it) within the size bound.
func personaFile(root, name string) (string, error) {
	clean := path.Clean(name)
	if name == "" || path.IsAbs(name) || clean != name || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("file %q: not a plain path inside the package", name)
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("file %q: hidden", name)
		}
	}
	p := filepath.Join(root, filepath.FromSlash(clean))
	st, err := os.Lstat(p)
	if err != nil {
		return "", fmt.Errorf("file %q: %w", name, err)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("file %q: not a regular file", name)
	}
	if st.Size() > personaMaxFile {
		return "", fmt.Errorf("file %q: %d bytes, more than %d", name, st.Size(), personaMaxFile)
	}
	return p, nil
}

// personaFiles is every regular file in the package (slash paths, sorted), dot files left out, and their total size.
func personaFiles(root string) ([]string, int64, error) {
	var files []string
	var total int64
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		total += info.Size()
		return nil
	})
	sort.Strings(files)
	return files, total, err
}

func (h *Hub) personaRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/personas", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"personas": h.personas()})
	})
	// A file of a package that checks out (any of its files: the manifest's, or a preview next to them).
	api.HandleFunc("GET /api/v1/personas/{id}/{file...}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := loadPersona(h.personasDir(), id); err != nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no persona %q", id))
			return
		}
		p, err := personaFile(filepath.Join(h.personasDir(), id), r.PathValue("file"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		f, err := os.Open(p)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		defer f.Close()
		ct := mime.TypeByExtension(path.Ext(p))
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.Copy(w, f)
	})
}
