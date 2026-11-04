package node

import (
	"os"
	"os/exec"
	"path/filepath"
)

// loginShellProgram is Git for Windows' bash, what Claude Code itself runs commands with on Windows
// (CLAUDE_CODE_GIT_BASH_PATH when set): the launcher scripts are POSIX shell. Never a bare "bash": on the PATH a
// service gets, System32's bash.exe comes first, and it runs WSL, another machine as far as this node goes. "" when
// there is no Git Bash: the command then fails to start rather than run somewhere else.
func loginShellProgram() string { return gitBash() }

func gitBash() string {
	exists := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" && exists(p) {
		return p
	}
	var cands []string
	if git, err := exec.LookPath("git"); err == nil {
		// <Git>\cmd\git.exe, <Git>\bin\git.exe or <Git>\mingw64\bin\git.exe
		dir := filepath.Dir(filepath.Dir(git))
		cands = append(cands, filepath.Join(dir, "bin", "bash.exe"), filepath.Join(filepath.Dir(dir), "bin", "bash.exe"))
	}
	for _, base := range []string{os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		if base != "" {
			cands = append(cands, filepath.Join(base, "Git", "bin", "bash.exe"))
		}
	}
	for _, c := range cands {
		if exists(c) {
			return c
		}
	}
	return ""
}
