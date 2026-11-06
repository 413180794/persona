package sandbox

import (
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ELFDependencies returns only exact loader/library files for a trusted native
// executable. It never runs ldd or mounts whole library/config directories.
func ELFDependencies(p string, seen map[string]bool) ([]string, error) {
	if seen[p] {
		return nil, nil
	}
	seen[p] = true
	f, err := elf.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var deps []string
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			b, err := io.ReadAll(prog.Open())
			if err != nil {
				return nil, err
			}
			deps = append(deps, strings.TrimRight(string(b), "\x00"))
		}
	}
	names, err := f.ImportedLibraries()
	if err != nil {
		return nil, err
	}
	arch := map[string]string{"amd64": "x86_64-linux-gnu", "arm64": "aarch64-linux-gnu"}[runtime.GOARCH]
	for _, name := range names {
		if filepath.Base(name) != name {
			return nil, errors.New("ELF dependency is not a library name")
		}
		found := ""
		for _, dir := range []string{"/lib/" + arch, "/usr/lib/" + arch, "/lib64", "/lib", "/usr/lib"} {
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
				found = candidate
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("runtime library unavailable: %s", name)
		}
		deps = append(deps, found)
	}
	var out []string
	for _, dep := range deps {
		if seen[dep] {
			continue
		}
		out = append(out, dep)
		nested, err := ELFDependencies(dep, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, nested...)
	}
	return out, nil
}
