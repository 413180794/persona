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
	return filepath.Join(home, ".persona")
}

func HomeDir() string {
	home, _ := os.UserHomeDir()
	return DataDir(home)
}

// PrepareHome creates only the configured state directory. Existing directory
// symlinks are accepted, but unavailable targets are never repaired or replaced.
func PrepareHome(home string) (string, error) {
	root := DataDir(home)
	if st, err := os.Lstat(root); err == nil && st.Mode()&os.ModeSymlink != 0 {
		target, err := os.Stat(root)
		if err != nil || !target.IsDir() {
			return "", fmt.Errorf("state directory %s has an unavailable directory symlink target", root)
		}
		return root, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func SocketDir(home string) string {
	return DataDir(home)
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

// isStateDir recognizes a directory under .persona or the configured PERSONA_HOME.
func isStateDir(path, name string) bool {
	path = filepath.Clean(path)
	if filepath.Base(path) != name {
		return false
	}
	if filepath.Base(filepath.Dir(path)) == ".persona" {
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

// NodeBinaryDir is where deploy.sh puts the node binaries next to the hub's own;
// PERSONA_NODE_BINARIES overrides it (Docker carries them in /opt/persona/nodes).
func NodeBinaryDir() string {
	if d := Getenv("PERSONA_NODE_BINARIES"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(DataDir(home), "bin", "nodes")
}
