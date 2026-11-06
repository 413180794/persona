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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
// manager.json had them), when, and for each the copy of those sections of hub.json kept beside it then. A section
// listed there is
// never moved again; one hub.json did not have stays unlisted, so an older build switched back that writes it there
// has it moved by the next start.
const MarkerKey = "moved_from_hub_json"

// Marker is MarkerKey's value.
type Marker struct {
	Keys []string  `json:"keys"`
	At   time.Time `json:"at"`
	// Backups are the copies of hub.json's sections, in the same directory, by the section moved when it was kept.
	Backups map[string]string `json:"backups,omitempty"`
}

// backupPrefix names the copies of hub.json's sections Move keeps (the node's path guard refuses every file named
// hub.json*). They hold only the sections taken over, never the rest of hub.json: the hub's token and database
// password do not belong in a manager's directory.
const backupPrefix = "hub.json.before-manager-json-"

// Report is what Move did.
type Report struct {
	// Moved are the sections it copied into manager.json this time.
	Moved []string
	// Backup is the copy of the sections it took over, as hub.json had them (a path; "" when it took over nothing).
	Backup string
	// Ignored are sections it took over that manager.json had already, with another value: manager.json's is kept
	// and read, hub.json's is not used (Backup has it).
	Ignored []string
	// Stale are sections hub.json has changed since they were moved: hub.json's copy is not read, manager.json's is.
	Stale []string
	// Untrimmed are the copies of a whole hub.json an earlier build kept that could not be cut down to their
	// sections (trimBackups), with why.
	Untrimmed []string
}

// Move copies the Moved sections of hubDir's hub.json that manager.json at file has not taken over yet into it, once,
// and records it (MarkerKey); a section manager.json has already keeps manager.json's value (Ignored when hub.json's
// differs). hub.json itself is not
// changed: an older build switched back still finds what it read before (later edits are manager.json's only). No
// hub.json, or nothing new to take over: manager.json is not touched. Stale lists the sections taken over whose value
// in hub.json is no longer what it was then (a hand edit of hub.json, or an older build's, which nothing reads).
func Move(hubDir, file string) (Report, error) {
	var rep Report
	hubPath := filepath.Join(hubDir, "hub.json")
	hubRaw, err := ReadFile(hubPath)
	if err == nil && hubRaw == nil {
		// Nothing to take over; the copies an earlier start kept are still cut down (no manager.json: none).
		if _, err := os.Lstat(file); err != nil {
			return rep, nil
		}
		err := editIf(file, func(o *Object) (bool, error) {
			m, err := readMarker(o)
			if err != nil {
				return false, fmt.Errorf("%s: %w", file, err)
			}
			rep.Untrimmed = trimBackups(filepath.Dir(file), m)
			return false, nil
		})
		return rep, err
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
		m, err := readMarker(o)
		if err != nil {
			return false, fmt.Errorf("%s: %w", file, err)
		}
		rep.Stale = stale(filepath.Dir(file), m, hub)
		rep.Untrimmed = trimBackups(filepath.Dir(file), m)
		var took []string
		for _, k := range Moved {
			v, inHub := hub.Get(k)
			if slices.Contains(m.Keys, k) || !inHub {
				continue
			}
			took = append(took, k)
			if have, mine := o.Get(k); !mine {
				o.SetRaw(k, v)
				rep.Moved = append(rep.Moved, k)
			} else if !sameJSON(have, v) {
				rep.Ignored = append(rep.Ignored, k)
			}
		}
		if len(took) == 0 {
			return false, nil
		}
		name := backupPrefix + now.Format("20060102T150405.000000000Z")
		if err := WriteFileAtomic(filepath.Join(filepath.Dir(file), name), Only(hub, took), 0o600); err != nil {
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

// readMarker is manager.json's MarkerKey (none: the zero Marker).
func readMarker(o *Object) (Marker, error) {
	var m Marker
	if raw, ok := o.Get(MarkerKey); ok {
		if err := json.Unmarshal(raw, &m); err != nil {
			return m, errors.New(MarkerKey + ": " + err.Error())
		}
	}
	return m, nil
}

// backupName is name when it names one of Move's copies, directly in the manager's directory; else "": a marker
// edited by hand or broken never points Move at another file there (manager.json itself, a hub.json).
func backupName(name string) string {
	if name != "" && name == filepath.Base(name) && strings.HasPrefix(name, backupPrefix) {
		return name
	}
	return ""
}

// trimBackups cuts the copies of hub.json in dir that hold more than the sections taken from them (an earlier build
// kept hub.json whole) down to those sections, so the hub's token and database password do not stay in the
// manager's directory: a copy the marker names, to the sections it names it for; one it doesn't (its manager.json
// was not written then), to the Moved ones. A temporary file one of them was being written to when a start was cut
// short (WriteFileAtomic's, beside it) is removed: Move writes them only under the directory's lock, which this
// holds. What it could not cut is returned, with why (a copy that is not JSON, or not a plain file, is left as it
// is, and said).
func trimBackups(dir string, m Marker) []string {
	byName := map[string][]string{}
	for _, k := range m.Keys {
		if name := backupName(m.Backups[k]); name != "" {
			byName[name] = append(byName[name], k)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var failed []string
	for _, e := range entries {
		name := e.Name()
		if backupName(name) == "" {
			continue
		}
		path := filepath.Join(dir, name)
		if strings.Contains(name, tempInfix) {
			if err := os.Remove(path); err != nil {
				failed = append(failed, path+": "+err.Error())
			}
			continue
		}
		if !e.Type().IsRegular() {
			failed = append(failed, path+": not a plain file")
			continue
		}
		keys, listed := byName[name]
		if !listed {
			keys = Moved
		}
		b, err := os.ReadFile(path)
		if err != nil {
			failed = append(failed, path+": "+err.Error())
			continue
		}
		o, err := ParseObject(b)
		if err != nil {
			failed = append(failed, path+": "+err.Error())
			continue
		}
		cut := Only(o, keys)
		if bytes.Equal(cut, o.Marshal()) {
			continue
		}
		if err := WriteFileAtomic(path, cut, 0o600); err != nil {
			failed = append(failed, path+": "+err.Error())
		}
	}
	return failed
}

// stale are the sections the marker took over whose hub.json value differs from the copy kept then (or which
// hub.json has dropped: nothing to warn about, it is not read either way).
func stale(dir string, m Marker, hub *Object) []string {
	copies := map[string]*Object{}
	var out []string
	for _, k := range m.Keys {
		name := backupName(m.Backups[k])
		if name == "" {
			continue
		}
		then, ok := copies[name]
		if !ok {
			if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
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

// sameJSON says whether a and b are the same JSON value, whatever their spacing or the order of their keys.
func sameJSON(a, b json.RawMessage) bool {
	ca, errA := canonicalJSON(a)
	cb, errB := canonicalJSON(b)
	if errA != nil || errB != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca, cb)
}

// canonicalJSON is raw re-encoded with its objects' keys sorted and its numbers as written.
func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// Sections reads the Moved sections of the manager.json at file as a JSON object (missing file: none), for a
// manager's configuration struct to unmarshal; identity and the marker are left out.
func Sections(file string) ([]byte, error) {
	b, err := readConfig(file)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return []byte("{}"), nil
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
