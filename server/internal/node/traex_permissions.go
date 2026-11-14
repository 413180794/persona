package node

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// TRAE CLI 0.215 accepts these as root options, including before `resume`. A permission preset and --sandbox
// cannot be combined. Empty mode keeps the launcher's defaults; plan never falls back to writable execution.
func traexPermissionArgs(mode, effort string) ([]string, error) {
	var args []string
	switch mode {
	case "":
	case "plan":
		args = []string{"--sandbox", "read-only", "--ask-for-approval", "never"}
	case proto.ModeBypass:
		args = []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}
	case "default", "acceptEdits":
		// TRAE's default preset permits workspace edits, with approval for access outside it.
		args = []string{"--permission-mode", "default"}
	case "auto":
		args = []string{"--permission-mode", "auto"}
	default:
		return nil, fmt.Errorf("unsupported permission mode %q for TRAE starts", mode)
	}
	switch effort {
	case "", effortAuto:
	case "low", "medium", "high", "xhigh":
		args = append(args, "--config", "model_reasoning_effort="+effort)
	default:
		return nil, fmt.Errorf("unsupported effort %q for TRAE starts; use auto, low, medium, high, or xhigh", effort)
	}
	return args, nil
}

func (n *Node) traexStartArgs(ctx context.Context, launcher, mode, effort string) ([]string, error) {
	args, err := traexPermissionArgs(mode, effort)
	if err != nil || mode != "plan" {
		return args, err
	}
	if _, managed := n.managedLauncher(launcher); managed {
		if err := n.ensureManaged(ctx, launcher); err != nil {
			return nil, err
		}
		l, ok := n.managedLauncher(launcher)
		if !ok || l.AgentType() != traexAgent || traexReadonlyConflict(l.Args) {
			return nil, readonlyStartUnsupported()
		}
	} else {
		// Probe freshly: cached metadata can predate an alias edit. Passing our options also proves a wrapper
		// forwards them; a launcher that drops, rearranges, or hides them has no verified read-only mapping.
		p, err := n.launcherProbe(ctx, launcher, args...)
		if err != nil || p.Agent != traexAgent || len(p.Args) < len(args) ||
			!slices.Equal(p.Args[len(p.Args)-len(args):], args) || traexReadonlyConflict(p.Args[:len(p.Args)-len(args)]) {
			return nil, readonlyStartUnsupported()
		}
	}
	return args, nil
}

// Do not guess precedence for a launcher's permission options, presets, subcommands or prompt delimiters.
// Values of model/config options are not flags. Unknown options fail closed only for read-only execution.
func traexReadonlyConflict(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, value, attached := strings.Cut(arg, "=")
		switch flag {
		case "--model", "--profile", "--image", "--cd", "--local-provider", "--enable", "--disable",
			"--remote-auth-token-env", "--disabled-tool", "--allowed-tool", "--disallowed-tool", "--shell-tool-timeout":
			if !attached {
				i++
				if i >= len(args) {
					return true
				}
			}
		case "--config":
			if !attached {
				i++
				if i >= len(args) {
					return true
				}
				value = args[i]
			}
			if traexReadonlyConfigConflict(value) {
				return true
			}
		case "--oss", "--search", "--no-alt-screen", "--strict-config", "--no-daemon":
		default:
			if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") || len(arg) == 1 {
				return true
			}
			for j := 1; j < len(arg); j++ {
				switch arg[j] {
				case 'm', 'p', 'i', 'C', 'c':
					value := strings.TrimPrefix(arg[j+1:], "=")
					if j+1 == len(arg) {
						i++
						if i >= len(args) {
							return true
						}
						value = args[i]
					}
					if arg[j] == 'c' && traexReadonlyConfigConflict(value) {
						return true
					}
					j = len(arg) // The rest belongs to the value, even when it resembles a flag.
				default:
					return true // Includes -s, -a, -y, and their attached/combined forms.
				}
			}
		}
	}
	return false
}

func traexReadonlyConfigConflict(value string) bool {
	key, _, _ := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	return strings.HasPrefix(key, "sandbox") || strings.HasPrefix(key, "approval") || strings.HasPrefix(key, "permissions") || key == "permission_mode"
}
