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

func IsAssistantDir(path string) bool {
	path = filepath.Clean(path)
	if filepath.Base(path) != "assistant" {
		return false
	}
	parent := filepath.Base(filepath.Dir(path))
	if parent == ".jarvis" || parent == ".persona" {
		return true
	}
	expected := filepath.Join(HomeDir(), "assistant")
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
