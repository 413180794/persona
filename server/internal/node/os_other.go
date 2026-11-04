//go:build !windows

package node

import (
	"errors"
	"os/exec"
	"path/filepath"
)

// progName is a command line's program as the scans compare it (isClaudeCmd, agentOfCmd): its base name.
func progName(argv0 string) string { return filepath.Base(argv0) }

// scanOrcaProcsOS and scanClaudeProcessesOS are Windows' scans; macOS and Linux use ps and /proc.
func scanOrcaProcsOS() (map[string][]orcaProc, error) { return nil, errors.New("no process scan here") }

func scanClaudeProcessesOS() map[int]procEnv { return map[int]procEnv{} }

// loginShellProgram is the interactive shell loginShell runs scripts in: zsh, so ~/.zshrc functions and PATH are
// there, else bash.
func loginShellProgram() string {
	if _, err := exec.LookPath("zsh"); err != nil {
		return "bash"
	}
	return "zsh"
}
