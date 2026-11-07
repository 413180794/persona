package hub

import (
	"testing"
)

// The manager is told where node builds come from: hub.json's node_packages.url, else this build's update URL
// (trailing slash dropped), "off" none; the proxy goes with it, and the environment's PERSONA_NODE_PACKAGES wins.
func TestDevicesConfigNodePackages(t *testing.T) {
	h := newTestHub(t)
	h.build.UpdateURL = "https://dl.example.com/dl/"
	if c := h.devicesConfig(); c.NodePackagesURL != "https://dl.example.com/dl" || c.NodePackagesProxy != "" {
		t.Fatalf("default: %+v", c)
	}
	h.cfg.NodePackages = NodePackagesConfig{URL: "https://mirror.example.com/persona/dl", Proxy: "http://[fd00::20]:3128"}
	if c := h.devicesConfig(); c.NodePackagesURL != "https://mirror.example.com/persona/dl" || c.NodePackagesProxy != "http://[fd00::20]:3128" {
		t.Fatalf("mirror: %+v", c)
	}
	h.cfg.NodePackages.URL = "off"
	if c := h.devicesConfig(); c.NodePackagesURL != "" || c.NodePackagesProxy != "" {
		t.Fatalf("off: %+v", c)
	}
	h.build.UpdateURL, h.cfg.NodePackages = "", NodePackagesConfig{}
	if c := h.devicesConfig(); c.NodePackagesURL != "" { // a build without an update URL, nothing set
		t.Fatalf("none: %+v", c)
	}
	t.Setenv("PERSONA_NODE_PACKAGES", "https://lan.example.com/dl")
	cfg := &Config{}
	cfg.applyEnv()
	if cfg.NodePackages.URL != "https://lan.example.com/dl" {
		t.Fatalf("environment: %+v", cfg.NodePackages)
	}
}
