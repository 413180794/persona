package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Editing hub.json the way the hub does (hub/admin_config.go, copied as it is): the usage sources page is the
// manager's now (admin_usage.go) and writes the usage section of the hub.json it shares with the hub, under the same
// lock on the directory, so an edit here and one on the hub's admin pages never write over each other.

// jsonObject is a JSON object that keeps its keys in order and every value as it was: an edit of hub.json changes the
// keys it means to and nothing else (fields this build does not know, written by a newer one or by hand, stay).
type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseJSONObject(b []byte) (*jsonObject, error) {
	o := &jsonObject{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(b)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("bad object key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if _, seen := o.vals[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = raw
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON object")
	}
	return o, nil
}

func (o *jsonObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *jsonObject) setRaw(key string, raw json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
}

func (o *jsonObject) set(key string, v any) error {
	raw, err := marshalNoEscape(v)
	if err != nil {
		return err
	}
	o.setRaw(key, raw)
	return nil
}

func (o *jsonObject) del(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// object is the nested object under key (empty when missing or null); setObject puts it back.
func (o *jsonObject) object(key string) (*jsonObject, error) {
	raw, ok := o.vals[key]
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return &jsonObject{vals: map[string]json.RawMessage{}}, nil
	}
	nested, err := parseJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return nested, nil
}

func (o *jsonObject) setObject(key string, nested *jsonObject) {
	if len(nested.keys) == 0 {
		o.del(key)
		return
	}
	o.setRaw(key, nested.marshal())
}

func (o *jsonObject) marshal() []byte {
	var b bytes.Buffer
	b.WriteString("{")
	for i, k := range o.keys {
		if i > 0 {
			b.WriteString(",")
		}
		key, _ := marshalNoEscape(k)
		b.Write(key)
		b.WriteString(":")
		b.Write(o.vals[k])
	}
	b.WriteString("}")
	return b.Bytes()
}

// indented is the file's text: two-space indent, one trailing newline.
func (o *jsonObject) indented() ([]byte, error) {
	var compact, out bytes.Buffer
	if err := json.Compact(&compact, o.marshal()); err != nil {
		return nil, err
	}
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func marshalNoEscape(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// hubJSONMu serializes this process's edits of hub.json; a lock on its directory does the same between processes
// (the hub, the manager and `jarvisd hub admin …` on its machine). Not on hub.json itself: the rename replaces that
// file.
var hubJSONMu sync.Mutex

// editHubJSON reads <dir>/hub.json (missing: an empty object), lets edit change it and writes it back atomically:
// a temporary file in the same directory, fsync, rename over the old one, mode 0600. A file that is not a JSON
// object is left alone (error). Values from the environment (JARVIS_PUBLIC_URL…) never get here: edit sees the file.
func editHubJSON(dir string, edit func(o *jsonObject) error) error {
	hubJSONMu.Lock()
	defer hubJSONMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(dir, "hub.json")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	o, err := parseJSONObject(b)
	if err != nil {
		return fmt.Errorf("%s: %w (not changed)", path, err)
	}
	if err := edit(o); err != nil {
		return err
	}
	out, err := o.indented()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	// The rename itself must survive a power cut too.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
