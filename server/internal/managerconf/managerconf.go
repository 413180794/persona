// Package managerconf is a manager's own configuration file, manager.json (docs/config.md "manager.json"): the
// sections of the configuration that only the manager uses (connectors, extensions, bots, mail sign-ins, Lark,
// usage sources), which the built-in manager used to read and write in the hub's hub.json. A manager made in the app
// keeps them in the manager.json of its own directory, next to its identity (id, hub, token); the built-in manager,
// which the hub starts, in DefaultDir. Move takes the sections out of hub.json once.
package managerconf

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// File is a manager's configuration file in its directory.
const File = "manager.json"

// DefaultDir is the built-in manager's directory (the assistant the hub starts): managers/assistant in the data
// directory home, where a manager made in the app has its own (managers/<id>). Its manager.json holds no identity: the
// hub hands that manager its token itself.
func DefaultDir(home string) string {
	return filepath.Join(home, "managers", assistantwire.AssistantID)
}

// DefaultFile is the built-in manager's manager.json.
func DefaultFile(home string) string { return filepath.Join(DefaultDir(home), File) }

// Moved are the top-level sections of hub.json that are the manager's: Move takes them into manager.json, and only
// the manager reads them there. A section added here later is moved by the next start (Marker lists what was).
var Moved = []string{
	"lark", "lark_gate", "connector_gate", "github", "imessage", "google", "microsoft", "bots",
	"extensions", "plugins", "trusted_extension_markets", "usage", "sent_ledger_days",
}

// MarkerKey is manager.json's record of the move: the sections it took over from hub.json (copied, or kept as
// manager.json had them), when, and for each the copy of hub.json kept beside it then. A section listed there is
// never moved again; one hub.json did not have stays unlisted, so an older build switched back that writes it there
// has it moved by the next start.
const MarkerKey = "moved_from_hub_json"

// Marker is MarkerKey's value.
type Marker struct {
	Keys []string  `json:"keys"`
	At   time.Time `json:"at"`
	// Backups are the copies of hub.json, in the same directory, by the section moved when it was kept.
	Backups map[string]string `json:"backups,omitempty"`
}

// backupPrefix names the copies of hub.json Move keeps (the node's path guard refuses every file named hub.json*).
const backupPrefix = "hub.json.before-manager-json-"

// Report is what Move did.
type Report struct {
	// Moved are the sections it copied into manager.json this time.
	Moved []string
	// Backup is the copy of hub.json it kept (a path; "" when it took over nothing).
	Backup string
	// Stale are sections hub.json has changed since they were moved: hub.json's copy is not read, manager.json's is.
	Stale []string
}

// Move copies the Moved sections of hubDir's hub.json that manager.json at file has not taken over yet into it, once,
// and records it (MarkerKey); a section manager.json has already keeps manager.json's value. hub.json itself is not
// changed: an older build switched back still finds what it read before (later edits are manager.json's only). No
// hub.json, or nothing new to take over: manager.json is not touched. Stale lists the sections taken over whose value
// in hub.json is no longer what it was then (a hand edit of hub.json, or an older build's, which nothing reads).
func Move(hubDir, file string) (Report, error) {
	var rep Report
	hubPath := filepath.Join(hubDir, "hub.json")
	hubRaw, err := os.ReadFile(hubPath)
	if errors.Is(err, os.ErrNotExist) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	hub, err := ParseObject(hubRaw)
	if err != nil {
		return rep, errors.New(hubPath + ": " + err.Error())
	}
	now := time.Now().UTC()
	err = editIf(file, func(o *Object) (bool, error) {
		var m Marker
		if raw, ok := o.Get(MarkerKey); ok {
			if err := json.Unmarshal(raw, &m); err != nil {
				return false, errors.New(MarkerKey + ": " + err.Error())
			}
		}
		rep.Stale = stale(filepath.Dir(file), m, hub)
		var took []string
		for _, k := range Moved {
			v, inHub := hub.Get(k)
			if slices.Contains(m.Keys, k) || !inHub {
				continue
			}
			took = append(took, k)
			if _, mine := o.Get(k); !mine {
				o.SetRaw(k, v)
				rep.Moved = append(rep.Moved, k)
			}
		}
		if len(took) == 0 {
			return false, nil
		}
		name := backupPrefix + now.Format("20060102T150405.000000000Z")
		if err := WriteFileAtomic(filepath.Join(filepath.Dir(file), name), hubRaw, 0o600); err != nil {
			return false, err
		}
		rep.Backup = filepath.Join(filepath.Dir(file), name)
		if m.Backups == nil {
			m.Backups = map[string]string{}
		}
		for _, k := range took {
			m.Backups[k] = name
		}
		m.Keys = append(m.Keys, took...)
		m.At = now
		raw, err := MarshalNoEscape(m)
		if err != nil {
			return false, err
		}
		o.SetRaw(MarkerKey, raw)
		return true, nil
	})
	return rep, err
}

// stale are the sections the marker took over whose hub.json value differs from the copy kept then (or which
// hub.json has dropped: nothing to warn about, it is not read either way).
func stale(dir string, m Marker, hub *Object) []string {
	copies := map[string]*Object{}
	var out []string
	for _, k := range m.Keys {
		name := m.Backups[k]
		if name == "" {
			continue
		}
		then, ok := copies[name]
		if !ok {
			if b, err := os.ReadFile(filepath.Join(dir, filepath.Base(name))); err == nil {
				then, _ = ParseObject(b)
			}
			copies[name] = then
		}
		if then == nil {
			continue
		}
		now, okNow := hub.Get(k)
		was, okThen := then.Get(k)
		if okNow && (!okThen || !sameJSON(now, was)) {
			out = append(out, k)
		}
	}
	return out
}

func sameJSON(a, b json.RawMessage) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// Sections reads the Moved sections of the manager.json at file as a JSON object (missing file: none), for a
// manager's configuration struct to unmarshal; identity and the marker are left out.
func Sections(file string) ([]byte, error) {
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return []byte("{}"), nil
	}
	if err != nil {
		return nil, err
	}
	o, err := ParseObject(b)
	if err != nil {
		return nil, errors.New(file + ": " + err.Error())
	}
	return Only(o, Moved), nil
}

// Only is o with only the keys in keys, compact.
func Only(o *Object, keys []string) []byte {
	out := &Object{vals: map[string]json.RawMessage{}}
	for _, k := range o.keys {
		if slices.Contains(keys, k) {
			out.SetRaw(k, o.vals[k])
		}
	}
	return out.Marshal()
}

// Without is o without the keys in keys, compact.
func Without(o *Object, keys []string) []byte {
	out := &Object{vals: map[string]json.RawMessage{}}
	for _, k := range o.keys {
		if !slices.Contains(keys, k) {
			out.SetRaw(k, o.vals[k])
		}
	}
	return out.Marshal()
}
