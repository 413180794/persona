package manager

import (
	"context"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/db"
)

// A manager as a service of its own (docs/managers.md, "Commands"): on Linux the systemd user unit
// jarvis-manager-<id>.service, on a Mac the launchd agent com.jarvis.manager.<id>; each runs
// `<binary> manager run --dir <its directory>` and is started again when it exits. In a container the container is
// the service (docker run --restart unless-stopped). Named like the hub's jarvisd and com.jarvis.hub.

// env is what the service commands touch, so tests can stand in for systemctl / launchctl.
type env struct {
	stdout io.Writer
	goos   string
	home   string
	uid    int
	user   string
	exe    func() (string, error)
	run    func(ctx context.Context, name string, args ...string) (string, error)
	look   func(name string) (string, error)
	getenv func(string) string
}

func newEnv(stdout io.Writer) *env {
	home, _ := os.UserHomeDir()
	e := &env{stdout: stdout, goos: runtime.GOOS, home: home, uid: os.Getuid(), exe: os.Executable,
		look: exec.LookPath, getenv: os.Getenv,
		run: func(ctx context.Context, name string, args ...string) (string, error) {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			return string(out), err
		}}
	if u, err := user.Current(); err == nil {
		e.user = u.Username
	}
	return e
}

// self is how the user runs this program, for the lines it prints.
func (e *env) self() string { return "personad" }

func unitName(id string) string     { return "jarvis-manager-" + id + ".service" }
func launchdLabel(id string) string { return "com.jarvis.manager." + id }

func (e *env) unitPath(id string) string {
	base := e.getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(e.home, ".config")
	}
	return filepath.Join(base, "systemd", "user", unitName(id))
}

func (e *env) plistPath(id string) string {
	return filepath.Join(e.home, "Library", "LaunchAgents", launchdLabel(id)+".plist")
}

// binary is what the service runs: the hub machine's <data dir>/bin/jarvisd when there is one (install.sh puts it
// there and self-update keeps it current), else this executable.
func (e *env) binary() (string, error) {
	bin := filepath.Join(dataDir(), "bin", "jarvisd")
	if fileExists(bin) {
		return bin, nil
	}
	exe, err := e.exe()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}

func systemdEnvQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(s) + `"`
}

// serviceUnit is a manager's systemd user unit. Exit status 2 is a usage or configuration mistake (no manager.json,
// a bad one): restarting cannot fix it.
func serviceUnit(id, bin, dir string) string {
	return `# Written by personad manager install; personad manager uninstall removes it (docs/managers.md).
[Unit]
Description=Persona manager ` + id + `
After=network-online.target

[Service]
ExecStart=` + systemdQuote(bin) + ` manager run --dir ` + systemdQuote(dir) + `
Environment=` + systemdEnvQuote("PERSONA_ASSISTANT_DEPLOY=service") + `
Environment=PATH=%h/.local/bin:%h/bin:/usr/local/bin:/usr/bin:/bin
Restart=always
RestartSec=5
RestartPreventExitStatus=2

[Install]
WantedBy=default.target
`
}

// servicePlist is a manager's launchd agent; its log is manager.log in its directory.
func servicePlist(id, bin, dir, home string) string {
	x := html.EscapeString
	path := strings.Join([]string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), "/opt/homebrew/bin",
		"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
	log := filepath.Join(dir, "manager.log")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + launchdLabel(id) + `</string>
  <key>ProgramArguments</key><array><string>` + x(bin) + `</string><string>manager</string><string>run</string><string>--dir</string><string>` + x(dir) + `</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>PERSONA_ASSISTANT_DEPLOY</key><string>service</string>
    <key>PATH</key><string>` + x(path) + `</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>` + x(log) + `</string>
  <key>StandardErrorPath</key><string>` + x(log) + `</string>
</dict></plist>
`
}

// dirFlag parses [--dir D] and finds the manager: D, or the one manager of this machine.
func dirFlag(e *env, name string, args []string) (string, *assistant.ManagerConfig, error) {
	fs := flag.NewFlagSet("manager "+name, flag.ContinueOnError)
	fs.SetOutput(e.stdout)
	dir := fs.String("dir", "", "管理者的目录（这台只有一个管理者时可以不写）")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return "", nil, err
	}
	if len(pos) != 0 {
		return "", nil, errUsage
	}
	d := *dir
	if d == "" {
		if d, err = onlyManager(); err != nil {
			return "", nil, err
		}
	}
	if d, err = filepath.Abs(d); err != nil {
		return "", nil, err
	}
	c, err := assistant.ReadManagerConfig(d)
	if err != nil {
		return "", nil, fmt.Errorf("%s 不是管理者的目录：%w", d, err)
	}
	return d, c, nil
}

func (e *env) systemctl(ctx context.Context, args ...string) (string, error) {
	return e.run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// noSystemd says why there is no systemd user instance; "" when there is one.
func (e *env) noSystemd(ctx context.Context) string {
	if _, err := e.look("systemctl"); err != nil {
		return "没有 systemctl"
	}
	if out, err := e.systemctl(ctx, "show-environment"); err != nil {
		if line := firstLine(out); line != "" {
			return "连不上你的 systemd 用户实例：" + line
		}
		return "连不上你的 systemd 用户实例"
	}
	return ""
}

func cmdInstall(ctx context.Context, e *env, args []string) error {
	dir, c, err := dirFlag(e, "install", args)
	if err != nil {
		return err
	}
	bin, err := e.binary()
	if err != nil {
		return err
	}
	manual := fmt.Sprintf("自己让它一直运行：%s manager run --dir %s（tmux、nohup，或者容器）", shellQuote(bin), shellQuote(dir))
	switch e.goos {
	case "linux":
		if why := e.noSystemd(ctx); why != "" {
			return fmt.Errorf("装不了常驻服务（%s）；%s", why, manual)
		}
		path := e.unitPath(c.ID)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(serviceUnit(c.ID, bin, dir)), 0o644); err != nil {
			return err
		}
		for _, step := range [][]string{{"daemon-reload"}, {"enable", unitName(c.ID)}, {"restart", unitName(c.ID)}} {
			if out, err := e.systemctl(ctx, step...); err != nil {
				return fmt.Errorf("systemctl --user %s 失败：%s", strings.Join(step, " "), firstLine(out+" "+err.Error()))
			}
		}
		fmt.Fprintf(e.stdout, "装好了：管理者「%s」（%s）是 systemd 用户服务 %s，已经启动。\n  日志：journalctl --user -u %s -f\n",
			c.Name, c.ID, unitName(c.ID), unitName(c.ID))
		if e.user != "" && !fileExists("/var/lib/systemd/linger/"+e.user) {
			fmt.Fprintf(e.stdout, "  没打开 linger：你退出登录后它会停。运行 sudo loginctl enable-linger %s 让它一直运行\n", e.user)
		}
		return nil
	case "darwin":
		path := e.plistPath(c.ID)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(servicePlist(c.ID, bin, dir, e.home)), 0o644); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(e.uid)
		_, _ = e.run(ctx, "launchctl", "bootout", domain+"/"+launchdLabel(c.ID))
		if out, err := e.run(ctx, "launchctl", "bootstrap", domain, path); err != nil {
			return fmt.Errorf("launchctl bootstrap 失败：%s", firstLine(out+" "+err.Error()))
		}
		fmt.Fprintf(e.stdout, "装好了：管理者「%s」（%s）是 launchd 服务 %s，登录后自动运行。\n  日志：%s\n",
			c.Name, c.ID, launchdLabel(c.ID), filepath.Join(dir, "manager.log"))
		return nil
	}
	return fmt.Errorf("%s 上还装不了常驻服务；%s", e.goos, manual)
}

func cmdUninstall(ctx context.Context, e *env, args []string) error {
	dir, c, err := dirFlag(e, "uninstall", args)
	if err != nil {
		return err
	}
	switch e.goos {
	case "linux":
		path := e.unitPath(c.ID)
		if !fileExists(path) {
			fmt.Fprintf(e.stdout, "管理者 %s 没有装成服务（没有 %s）\n", c.ID, path)
			return nil
		}
		_, _ = e.systemctl(ctx, "disable", "--now", unitName(c.ID))
		if err := os.Remove(path); err != nil {
			return err
		}
		_, _ = e.systemctl(ctx, "daemon-reload")
	case "darwin":
		path := e.plistPath(c.ID)
		if !fileExists(path) {
			fmt.Fprintf(e.stdout, "管理者 %s 没有装成服务（没有 %s）\n", c.ID, path)
			return nil
		}
		_, _ = e.run(ctx, "launchctl", "bootout", "gui/"+strconv.Itoa(e.uid)+"/"+launchdLabel(c.ID))
		if err := os.Remove(path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s 上没有装常驻服务", e.goos)
	}
	fmt.Fprintf(e.stdout, "停掉并去掉了管理者 %s 的服务。目录 %s 和它的数据库留着；在 App 里删掉它，hub 才不再认它的 token。\n", c.ID, dir)
	return nil
}

func cmdStatus(ctx context.Context, e *env, args []string) error {
	var dirs []string
	if len(args) > 0 {
		d, _, err := dirFlag(e, "status", args)
		if err != nil {
			return err
		}
		dirs = []string{d}
	} else {
		dirs = localManagers()
	}
	if len(dirs) == 0 {
		fmt.Fprintf(e.stdout, "%s 下没有管理者\n", managersRoot())
		return nil
	}
	hubDB := assistant.HubDatabaseURL()
	for _, d := range dirs {
		c, err := assistant.ReadManagerConfig(d)
		if err != nil {
			fmt.Fprintf(e.stdout, "%s：%v\n", d, err)
			continue
		}
		database := "（没有：--database-url）"
		if u, err := c.Database(hubDB); err == nil {
			database = db.DatabaseName(u)
		}
		fmt.Fprintf(e.stdout, "管理者「%s」（%s）\n  目录：%s\n  hub：%s\n  数据库：%s\n  服务：%s\n", c.Name, c.ID, d, c.Hub, database, e.serviceState(ctx, c.ID))
	}
	return nil
}

// serviceState is a line on the manager's service.
func (e *env) serviceState(ctx context.Context, id string) string {
	switch e.goos {
	case "linux":
		if !fileExists(e.unitPath(id)) {
			return "没装（personad manager install）"
		}
		out, _ := e.systemctl(ctx, "is-active", unitName(id))
		return unitName(id) + " " + strings.TrimSpace(out)
	case "darwin":
		if !fileExists(e.plistPath(id)) {
			return "没装（personad manager install）"
		}
		if _, err := e.run(ctx, "launchctl", "print", "gui/"+strconv.Itoa(e.uid)+"/"+launchdLabel(id)); err != nil {
			return launchdLabel(id) + " 没在运行"
		}
		return launchdLabel(id) + " 在运行"
	}
	return "这台系统上不装服务"
}
