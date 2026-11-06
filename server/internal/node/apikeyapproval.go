package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// An interactive Claude Code that finds ANTHROPIC_API_KEY set and has not seen that key before stops at "Detected a
// custom API key in your environment … Do you want to use this API key?", with "No (recommended)" selected. Enter (or
// the phone's first option) answers No, Claude remembers it, and from then on every claude of that launcher has no
// credentials (Not logged in). A managed launcher's key is the one the user entered for it in Settings › Models, so
// `personad launch` records it as approved before claude starts, as answering Yes does: the key's last 20 characters
// (how Claude Code names a key) in customApiKeyResponses.approved of Claude's global config, and out of rejected.
// Print mode (-p, the assistant) never asks, so this only matters to sessions in a terminal.

// claudeKeySuffix is how Claude Code stores a key it was asked about: the last 20 characters of the trimmed key.
func claudeKeySuffix(key string) string {
	key = strings.TrimSpace(key)
	if len(key) > 20 {
		return key[len(key)-20:]
	}
	return key
}

// claudeGlobalConfig is the global config file Claude Code reads for an environment, by its own rule: the legacy
// .config.json in its config directory when that is in use (not empty, or writable while the new file does not exist
// yet), else .claude.json in CLAUDE_CONFIG_DIR or the home directory (.claude-custom-oauth.json with
// CLAUDE_CODE_CUSTOM_OAUTH_URL).
func claudeGlobalConfig(env []string) string {
	get := func(k string) string { return envValue(env, k) }
	home := get("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	cfgDir, base := get("CLAUDE_CONFIG_DIR"), get("CLAUDE_CONFIG_DIR")
	if cfgDir == "" {
		cfgDir, base = filepath.Join(home, ".claude"), home
	}
	name := ".claude.json"
	if get("CLAUDE_CODE_CUSTOM_OAUTH_URL") != "" {
		name = ".claude-custom-oauth.json"
	}
	main := filepath.Join(base, name)
	legacy := filepath.Join(cfgDir, ".config.json")
	if st, err := os.Stat(legacy); err == nil && st.Mode().IsRegular() {
		_, err := os.Stat(main)
		if st.Size() > 0 || (st.Mode().Perm()&0o222 != 0 && os.IsNotExist(err)) {
			return legacy
		}
	}
	return main
}

// envValue is k's value in env as claude will see it: syscall.Exec passes env as it is, and getenv takes the first.
func envValue(env []string, k string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, k+"=") {
			return kv[len(k)+1:]
		}
	}
	return ""
}

// approveClaudeAPIKey records the ANTHROPIC_API_KEY of env as approved in Claude Code's global config. Nothing is
// written when there is no key, the key is approved already, or there is no config yet (a first run shows Claude's
// own onboarding anyway, with the user there).
func approveClaudeAPIKey(env []string) error {
	suffix := claudeKeySuffix(envValue(env, "ANTHROPIC_API_KEY"))
	if suffix == "" {
		return nil
	}
	path := claudeGlobalConfig(env)
	// Most launches find the key approved already: that is read without the lock, so they never wait on a claude. A
	// read that fails here (a claude halfway through a save) is done again under the lock.
	if c, err := readClaudeKeyResponses(path); err == nil && (c == nil || c.approves(suffix)) {
		return nil
	}
	unlock, err := lockClaudeConfig(path)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := readClaudeKeyResponses(path) // again, under the lock: what a claude saved meanwhile is kept
	if err != nil || c == nil || c.approves(suffix) {
		return err
	}
	return c.writeApproved(suffix)
}

// claudeKeyResponses is Claude Code's global config as read, with its customApiKeyResponses lists.
type claudeKeyResponses struct {
	path               string // symlinks resolved: the file that is replaced
	cfg, resp          map[string]json.RawMessage
	approved, rejected []string
}

// readClaudeKeyResponses reads the config at path; nil when there is none.
func readClaudeKeyResponses(path string) (*claudeKeyResponses, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := &claudeKeyResponses{path: path, cfg: map[string]json.RawMessage{}}
	if err := json.Unmarshal(b, &c.cfg); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if raw, ok := c.cfg["customApiKeyResponses"]; ok {
		if err := json.Unmarshal(raw, &c.resp); err != nil {
			return nil, fmt.Errorf("customApiKeyResponses in %s: %w", path, err)
		}
	}
	if c.resp == nil {
		c.resp = map[string]json.RawMessage{}
	}
	if raw, ok := c.resp["approved"]; ok {
		_ = json.Unmarshal(raw, &c.approved)
	}
	if raw, ok := c.resp["rejected"]; ok {
		_ = json.Unmarshal(raw, &c.rejected)
	}
	return c, nil
}

func (c *claudeKeyResponses) approves(suffix string) bool {
	return slices.Contains(c.approved, suffix) && !slices.Contains(c.rejected, suffix)
}

// writeApproved puts suffix in approved and out of rejected, keeping the rest of the config and the file's mode, and
// replaces the file through a temporary one.
func (c *claudeKeyResponses) writeApproved(suffix string) error {
	approved := c.approved
	if !slices.Contains(approved, suffix) {
		approved = append(approved, suffix)
	}
	rejected := slices.DeleteFunc(slices.Clone(c.rejected), func(s string) bool { return s == suffix })
	if rejected == nil {
		rejected = []string{}
	}
	c.resp["approved"], _ = json.Marshal(approved)
	c.resp["rejected"], _ = json.Marshal(rejected)
	c.cfg["customApiKeyResponses"], _ = json.Marshal(c.resp)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep <, >, & in the user's values as Claude writes them
	enc.SetIndent("", "  ")
	if err := enc.Encode(c.cfg); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(c.path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".claude.json.persona-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}

// Claude Code saves its global config under a lock: proper-lockfile's directory at "<config path>.lock" (the path as
// it names it, before symlinks), re-reading the file once it holds it. Taking the same lock around our read and rename
// keeps a running claude's save and this approval from overwriting each other. proper-lockfile counts a lock whose
// directory is older than 10 s as stale (its holder refreshes it every 5 s), so such a lock is taken over; one that
// cannot be removed is waited on like any other, until the deadline.
const claudeConfigLockStale = 10 * time.Second

var claudeConfigLockWait = 5 * time.Second // a var for the tests

func lockClaudeConfig(path string) (unlock func(), err error) {
	lock := path + ".lock"
	deadline := time.Now().Add(claudeConfigLockWait)
	for {
		err := os.Mkdir(lock, 0o777)
		if err == nil {
			return func() { _ = os.Remove(lock) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if st, err := os.Stat(lock); err == nil && time.Since(st.ModTime()) > claudeConfigLockStale {
			if os.Remove(lock) == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s is held by another process", lock)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
