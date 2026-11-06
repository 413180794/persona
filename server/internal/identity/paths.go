package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func explicitRoot(home string) string {
	if root := Getenv("PERSONA_HOME"); root != "" {
		if root == "~" {
			return home
		}
		if strings.HasPrefix(root, "~/") {
			return filepath.Join(home, root[2:])
		}
		if !filepath.IsAbs(root) {
			return filepath.Join(home, root)
		}
		return filepath.Clean(root)
	}
	return ""
}

func DataDir(home string) string {
	if root := explicitRoot(home); root != "" {
		return root
	}
	current, legacy := filepath.Join(home, ".persona"), filepath.Join(home, ".jarvis")
	if _, err := os.Lstat(legacy); err == nil {
		if a, err := os.Stat(current); err == nil {
			if b, err := os.Stat(legacy); err == nil && os.SameFile(a, b) {
				return current
			}
		}
		return legacy
	}
	return current
}

func HomeDir() string {
	home, _ := os.UserHomeDir()
	return DataDir(home)
}

func directoryExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func directoryAlias(home, source, alias string) error {
	err := os.Symlink(filepath.Base(source), alias)
	if err == nil || os.IsExist(err) {
		a, sourceErr := os.Stat(source)
		b, aliasErr := os.Stat(alias)
		if sourceErr == nil && aliasErr == nil && os.SameFile(a, b) {
			return nil
		}
		return fmt.Errorf("state directories changed during initialization; retry without moving either directory")
	}
	if directoryExists(DataDir(home)) {
		return nil
	}
	return fmt.Errorf("create compatibility directory %s: %w", alias, err)
}

func ensureDirectory(root, current, legacy string) error {
	if directoryExists(root) {
		return nil
	}
	st, err := os.Lstat(root)
	if err == nil && st.Mode()&os.ModeSymlink != 0 {
		if directoryExists(root) {
			return nil
		}
		if root != current && root != legacy {
			return fmt.Errorf("state directory %s has an unavailable symlink target", root)
		}
		target, err := os.Readlink(root)
		if err != nil {
			return err
		}
		raw := target
		if !filepath.IsAbs(raw) {
			raw = filepath.Dir(root) + string(os.PathSeparator) + raw
		}
		for strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, "/.") {
			if strings.HasSuffix(raw, "/.") {
				raw = strings.TrimSuffix(raw, "/.")
			} else {
				raw = strings.TrimSuffix(raw, "/")
			}
		}
		target = filepath.Clean(raw)
		if target != current && target != legacy {
			return fmt.Errorf("state directory %s has an unavailable symlink target", root)
		}
		cut := strings.LastIndex(raw, string(os.PathSeparator))
		if cut < 0 {
			return fmt.Errorf("state directory %s has an unavailable symlink target", root)
		}
		parent, parentErr := filepath.EvalSymlinks(raw[:cut])
		expected, expectedErr := filepath.EvalSymlinks(filepath.Dir(target))
		if parentErr != nil || expectedErr != nil || parent != expected {
			return fmt.Errorf("state directory %s has an unavailable symlink target", root)
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			if directoryExists(root) {
				return nil
			}
			return fmt.Errorf("state directory %s has an unavailable symlink target", root)
		}
		if err := os.MkdirAll(target, 0o700); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(root, 0o700)
}

func PrepareHome(home string) (string, error) {
	root := DataDir(home)
	current, legacy := filepath.Join(home, ".persona"), filepath.Join(home, ".jarvis")
	if root != current && root != legacy {
		if err := ensureDirectory(root, current, legacy); err != nil {
			return "", err
		}
		return root, nil
	}
	_, currentErr := os.Lstat(current)
	legacyInfo, legacyErr := os.Lstat(legacy)
	if os.IsNotExist(currentErr) && (os.IsNotExist(legacyErr) || legacyErr == nil && (legacyInfo.Mode()&os.ModeSymlink == 0 || directoryExists(legacy))) {
		if err := os.MkdirAll(legacy, 0o700); err != nil {
			return "", err
		}
		if err := directoryAlias(home, legacy, current); err != nil {
			return "", err
		}
		return DataDir(home), nil
	}
	if os.IsNotExist(legacyErr) && directoryExists(current) {
		if err := directoryAlias(home, current, legacy); err != nil {
			return "", err
		}
		return DataDir(home), nil
	}
	if err := ensureDirectory(root, current, legacy); err != nil {
		return "", err
	}
	alias := legacy
	if root == legacy {
		alias = current
	}
	if _, err := os.Lstat(alias); os.IsNotExist(err) {
		if err := directoryAlias(home, root, alias); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return DataDir(home), nil
}

func SocketDir(home string) string {
	root := DataDir(home)
	legacy := filepath.Join(home, ".jarvis")
	if root == filepath.Join(home, ".persona") {
		a, rootErr := os.Stat(root)
		b, legacyErr := os.Stat(legacy)
		if rootErr == nil && legacyErr == nil && os.SameFile(a, b) {
			return legacy
		}
	}
	return root
}

func AssistantDir(home string) string {
	return filepath.Join(DataDir(home), "assistant")
}

// HealthDir is the health space's directory (docs/health.md): the assistant's conversations about health run there,
// next to its own directory, so their memory and instructions are their own.
func HealthDir(home string) string {
	return filepath.Join(DataDir(home), SpaceHealth)
}

// SpaceHealth is the health space's name: its directory's and Session.Space.
const SpaceHealth = "health"

// builtinManager is the id of the manager the hub starts (assistantwire.AssistantID): its directories keep their
// names.
const builtinManager = "assistant"

// AssistantDirName is the name in the state directory of manager id's assistant directory: "assistant" for the
// built-in manager's ("" too), "assistant-<id>" for another's (docs/managers.md "Directories on a computer").
func AssistantDirName(id string) string { return managerDirName("assistant", id) }

// HealthDirName is AssistantDirName for its health space: "health", or "health-<id>".
func HealthDirName(id string) string { return managerDirName(SpaceHealth, id) }

func managerDirName(base, id string) string {
	if id == "" || id == builtinManager {
		return base
	}
	return base + "-" + id
}

// IsManagerDirName: name is base or base-<id> for a manager id the hub makes ("m-" and hex digits). Other names that
// start the same (assistant-bin, assistant-hooks) are not.
func IsManagerDirName(name, base string) bool {
	if name == base {
		return true
	}
	hex, ok := strings.CutPrefix(name, base+"-m-")
	return ok && hex != "" && strings.Trim(hex, "0123456789abcdef") == ""
}

// IsAssistantDir: path is one of the assistant's directories, its own or the health space's, of any manager.
func IsAssistantDir(path string) bool {
	return isManagerStateDir(path, "assistant") || isManagerStateDir(path, SpaceHealth)
}

// IsAssistantDirOf: path is one of manager id's assistant directories, its own or its health space's (another
// manager's, left on a computer moved away from it, is not).
func IsAssistantDirOf(path, id string) bool {
	return isStateDir(path, AssistantDirName(id)) || isStateDir(path, HealthDirName(id))
}

// SpaceOf is the space of an assistant directory: SpaceHealth for a health space's, "" for the assistant's own.
func SpaceOf(path string) string {
	if isManagerStateDir(path, SpaceHealth) {
		return SpaceHealth
	}
	return ""
}

// isManagerStateDir: path is the directory base, or base-<id> (IsManagerDirName), in the state directory.
func isManagerStateDir(path, base string) bool {
	name := filepath.Base(filepath.Clean(path))
	return IsManagerDirName(name, base) && isStateDir(path, name)
}

// isStateDir: path is the directory name in the state directory (~/.persona, ~/.jarvis or PERSONA_HOME's).
func isStateDir(path, name string) bool {
	path = filepath.Clean(path)
	if filepath.Base(path) != name {
		return false
	}
	parent := filepath.Base(filepath.Dir(path))
	if parent == ".jarvis" || parent == ".persona" {
		return true
	}
	expected := filepath.Join(HomeDir(), name)
	if path == expected {
		return true
	}
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stat(expected)
	return err == nil && os.SameFile(a, b)
}

// NodeBinaryDir is where deploy.sh puts the node binaries next to the hub's own (the hub's updates put the release's
// there too, the manager deploys them from there); JARVIS_NODE_BINARIES points elsewhere (the Docker image carries
// them in /opt/jarvis/nodes: ~/.jarvis is the data volume there).
func NodeBinaryDir() string {
	if d := Getenv("JARVIS_NODE_BINARIES"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(DataDir(home), "bin", "nodes")
}
