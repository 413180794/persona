package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
)

// Installation identities are selected once from existing resources, never migrated
// by an install/status command. A rolling upgrade must not provision a second node.
const (
	legacyLinkUnit   = "jarvis-link.service"
	legacyNodeUnit   = "jarvisd-node.service"
	legacyNodeLabel  = "com.jarvis.node"
	legacyTaskFile   = "jarvis-link-task.xml"
	legacyServiceExe = "jarvis-link-service.exe"
	legacyDaemonFlag = "--jarvisd"
	legacyHubService = "_jarvis-hub._tcp"
	legacyLinkBinary = "jarvis-link"
	legacyNodeBinary = "jarvisd"
)

func compatibleFlag(a, name string) string {
	if name == "--personad" {
		if a == legacyDaemonFlag {
			return name
		}
		if v, ok := strings.CutPrefix(a, legacyDaemonFlag+"="); ok {
			return name + "=" + v
		}
	}
	return a
}

func existingPath(canonical, legacy string) string {
	if fileExists(legacy) {
		return legacy
	}
	return canonical
}

func (e *env) linkUnitName() string     { return filepath.Base(e.unitPath()) }
func (e *env) residentUnitName() string { return filepath.Base(e.nodeUnitPath()) }
func (e *env) residentLabel() string {
	return strings.TrimSuffix(filepath.Base(e.nodePlistPath()), ".plist")
}

// Refuse ambiguous independent installations instead of picking one and restarting it.
func (e *env) installationConflict(node bool) error {
	var dir, canonical, legacy string
	switch {
	case e.goos == "linux":
		dir = filepath.Dir(e.unitPath())
		canonical, legacy = unitName, legacyLinkUnit
		if node {
			canonical, legacy = nodeUnitName, legacyNodeUnit
		}
	case e.goos == "darwin" && node:
		dir = filepath.Join(e.home, "Library", "LaunchAgents")
		canonical, legacy = nodeLabel+".plist", legacyNodeLabel+".plist"
	default:
		return nil
	}
	if a, err := os.Stat(filepath.Join(dir, canonical)); err == nil {
		if b, err := os.Stat(filepath.Join(dir, legacy)); err == nil && !os.SameFile(a, b) {
			return i18n.Err("link-compatibility.installation_conflict", i18n.Args{"current": canonical, "legacy": legacy})
		}
	}
	return nil
}
