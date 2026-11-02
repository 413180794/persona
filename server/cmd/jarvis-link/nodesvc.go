package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The resident node (`jarvisd node --attach`, docs/server.md「常驻 node」) is what the connector forwards to. Setting
// it up here is packaging only, so that one command (Linux: `jarvis-link install`) or one app (the Mac menu bar app)
// sets a computer up as the node service: it writes the same service scripts/node-launchd.sh does, and leaves one
// that is already there as it is. What the node does stays in jarvisd.

const (
	nodeUnitName = "jarvisd-node.service" // systemd user unit (Linux), as scripts/node-launchd.sh names it
	nodeLabel    = "com.jarvis.node"      // launchd agent (Mac), likewise
)

func (e *env) nodeBin() string { return filepath.Join(e.jarvisDir(), "bin", "jarvisd") }
func (e *env) nodeLog() string { return filepath.Join(e.jarvisDir(), "node-attach.log") }

func (e *env) nodeUnitPath() string { return filepath.Join(filepath.Dir(e.unitPath()), nodeUnitName) }
func (e *env) nodePlistPath() string {
	return filepath.Join(e.home, "Library", "LaunchAgents", nodeLabel+".plist")
}

// nodeServiceFile is where this computer's resident node service is described; "" where there is none to write.
func (e *env) nodeServiceFile() string {
	switch e.goos {
	case "linux":
		return e.nodeUnitPath()
	case "darwin":
		return e.nodePlistPath()
	}
	return ""
}

// nodeUnitFile is the resident node's systemd user unit: scripts/node-launchd.sh's, without network-online.target
// (a user instance has none, docs/lessons/server.md).
func nodeUnitFile(bin, logPath string) string {
	return `# Written by jarvis-link (docs/link.md); jarvis-link node uninstall removes it.
[Unit]
Description=Jarvis resident node (docs/server.md)

[Service]
ExecStart=` + systemdQuote(bin) + ` node --attach
Restart=always
RestartSec=10
StandardError=append:` + strings.ReplaceAll(logPath, "%", "%%") + `

[Install]
WantedBy=default.target
`
}

// nodePlist is the resident node's launchd agent: scripts/node-launchd.sh's.
func nodePlist(bin, logPath string) string {
	x := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + nodeLabel + `</string>
  <key>ProgramArguments</key><array><string>` + x(bin) + `</string><string>node</string><string>--attach</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardErrorPath</key><string>` + x(logPath) + `</string>
</dict></plist>
`
}

// nodeInstall sets up the resident node service unless it is there already: jarvisd at ~/.jarvis/bin/jarvisd (copied
// from from when there is none yet), then the service, started.
func nodeInstall(e *env, from string) error {
	file := e.nodeServiceFile()
	if file == "" {
		return fmt.Errorf("%s 上还装不了常驻 node 的服务（#299）", e.goos)
	}
	if fileExists(file) {
		fmt.Fprintf(e.stdout, "常驻 node 的服务已经装了（%s），保持原样。\n", file)
		return nil
	}
	bin := e.nodeBin()
	if err := os.MkdirAll(e.jarvisDir(), 0o700); err != nil {
		return err
	}
	if !fileExists(bin) {
		if from == "" {
			return fmt.Errorf("没有 %s：先装 jarvisd（安装脚本会一起装），或者用 --jarvisd <路径> 指定", bin)
		}
		if err := copyExecutable(from, bin); err != nil {
			return fmt.Errorf("放不进 %s：%w", bin, err)
		}
		fmt.Fprintf(e.stdout, "装好了 %s。\n", bin)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	switch e.goos {
	case "linux":
		if why := e.systemdMissing(); why != "" {
			return fmt.Errorf("没有 systemd 用户服务（%s）：nohup %s node --attach >> %s 2>&1 &", why, shellQuote(bin), shellQuote(e.nodeLog()))
		}
		if err := os.WriteFile(file, []byte(nodeUnitFile(bin, e.nodeLog())), 0o644); err != nil {
			return err
		}
		for _, step := range [][]string{{"daemon-reload"}, {"enable", "--now", nodeUnitName}} {
			if out, err := e.systemctl(step...); err != nil {
				return fmt.Errorf("systemctl --user %s 失败：%s", strings.Join(step, " "), orErr(out, err))
			}
		}
	case "darwin":
		if err := os.WriteFile(file, []byte(nodePlist(bin, e.nodeLog())), 0o644); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(e.uid)
		_, _ = e.command("launchctl", "bootout", domain+"/"+nodeLabel)
		if out, err := e.command("launchctl", "bootstrap", domain, file); err != nil {
			return fmt.Errorf("launchctl bootstrap 失败：%s", orErr(out, err))
		}
	}
	fmt.Fprintf(e.stdout, "装好了常驻 node 的服务（%s），已启动，日志在 %s。\n", file, e.nodeLog())
	return nil
}

// nodeState is `jarvis-link node status --json`.
type nodeState struct {
	Installed bool   `json:"installed"`
	File      string `json:"file,omitempty"`
	Bin       string `json:"bin"`
	HasBin    bool   `json:"has_bin"`
	// Running: launchd has the agent loaded / systemd says active; unknown ("") where that cannot be asked.
	Running string `json:"running,omitempty"`
}

func nodeStatus(e *env) nodeState {
	st := nodeState{File: e.nodeServiceFile(), Bin: e.nodeBin(), HasBin: fileExists(e.nodeBin())}
	st.Installed = st.File != "" && fileExists(st.File)
	if !st.Installed {
		return st
	}
	switch e.goos {
	case "linux":
		if e.systemdMissing() == "" {
			out, _ := e.systemctl("is-active", nodeUnitName)
			st.Running = firstLine(out)
		}
	case "darwin":
		// Read-only: whether launchd has it. Not by connecting to the node's socket, which would take the node from the hub.
		if _, err := e.command("launchctl", "print", "gui/"+strconv.Itoa(e.uid)+"/"+nodeLabel); err == nil {
			st.Running = "loaded"
		} else {
			st.Running = "not loaded"
		}
	}
	return st
}

// nodeCmd is `jarvis-link node install [--jarvisd <path>] | status [--json] | uninstall`.
func nodeCmd(e *env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "install":
		var from string
		for i := 1; i < len(args); i++ {
			v, next, ok, err := flagValue(args, i, "--jarvisd")
			switch {
			case err != nil:
				return err
			case !ok:
				return errUsage
			}
			from, i = v, next
		}
		return nodeInstall(e, from)
	case "status":
		st := nodeStatus(e)
		if len(args) > 1 && args[1] == "--json" {
			enc := json.NewEncoder(e.stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		switch {
		case st.File == "":
			fmt.Fprintf(e.stdout, "常驻 node：%s 上还没有服务可装（#299）\n", e.goos)
		case !st.Installed:
			fmt.Fprintf(e.stdout, "常驻 node：服务没装（jarvis-link node install）\n")
		default:
			fmt.Fprintf(e.stdout, "常驻 node：服务装了（%s）%s\n", st.File, map[bool]string{true: " · " + st.Running, false: ""}[st.Running != ""])
		}
		if !st.HasBin {
			fmt.Fprintf(e.stdout, "没有 %s\n", st.Bin)
		}
		if st.Installed {
			return nil
		}
		return exitCode(1)
	case "uninstall":
		file := e.nodeServiceFile()
		if file == "" || !fileExists(file) {
			fmt.Fprintln(e.stdout, "常驻 node 的服务没装。")
			return nil
		}
		switch e.goos {
		case "linux":
			if e.systemdMissing() == "" {
				_, _ = e.systemctl("disable", "--now", nodeUnitName)
			}
		case "darwin":
			_, _ = e.command("launchctl", "bootout", "gui/"+strconv.Itoa(e.uid)+"/"+nodeLabel)
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if e.goos == "linux" {
			_, _ = e.systemctl("daemon-reload")
		}
		fmt.Fprintf(e.stdout, "停掉并删掉了常驻 node 的服务（%s）；%s 留着。\n", file, e.nodeBin())
		return nil
	}
	return errUsage
}

// copyExecutable puts a copy of src at dst (0755), whole or not at all.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
