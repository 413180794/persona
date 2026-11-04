package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/nwverse/persona/server/internal/plugin"
)

// `personad plugin …` on the hub's machine (docs/plugins.md「命令行」): what installs, upgrades and removes plugins.
// It talks to the running hub over loopback with the master token in hub.json, like `hub admin`.

const pluginUsage = `usage:
  personad plugin list                          the plugins and their state
  personad plugin show <id>                     one plugin: permissions, settings, secrets set or not
  personad plugin install <source> [--yes]      install (source: a directory, dir:/p, file:/p.tar.gz | .zip,
                                                https://…/p.tar.gz#sha256=<hex>, remote:rpc=/p.sock,http=/q.sock);
                                                shows what it may do and asks first unless --yes
  personad plugin upgrade <id> <source> [--yes] a newer version of an installed plugin
  personad plugin rollback <id>                 back to the version before the last upgrade
  personad plugin enable <id> | disable <id>
  personad plugin config <id> key=value …       set settings (a value is JSON when it parses, text otherwise)
  personad plugin secret <id> <name>            set a secret, read from stdin (never an argument)
  personad plugin secret <id> <name> --delete
  personad plugin uninstall <id> [--keep-data]`

// PluginCommand runs `personad plugin <args>`.
func PluginCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(pluginUsage)
	}
	cfg, err := localHubConfig()
	if err != nil {
		return err
	}
	c := &pluginClient{cfg: cfg}
	flag := func(name string) bool {
		for _, a := range args[1:] {
			if a == name {
				return true
			}
		}
		return false
	}
	var positional []string
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "--") {
			positional = append(positional, a)
		}
	}
	need := func(n int) error {
		if len(positional) < n {
			return errors.New(pluginUsage)
		}
		return nil
	}
	switch args[0] {
	case "list":
		var out struct {
			Plugins []PluginView `json:"plugins"`
		}
		if err := c.do(ctx, "GET", "/api/v1/plugins", nil, &out); err != nil {
			return err
		}
		if len(out.Plugins) == 0 {
			fmt.Fprintln(stdout, "没有插件")
		}
		for _, p := range out.Plugins {
			line := fmt.Sprintf("%-20s %-10s %-9s %-12s %s", p.ID, p.Version, p.Source, p.Status, p.Name)
			if p.Error != "" {
				line += "（" + p.Error + "）"
			}
			if len(p.NeedsSetup) > 0 {
				line += "；还缺：" + strings.Join(p.NeedsSetup, "、")
			}
			fmt.Fprintln(stdout, strings.TrimRight(line, " "))
		}
		return nil
	case "show":
		if err := need(1); err != nil {
			return err
		}
		var d PluginDetail
		if err := c.do(ctx, "GET", "/api/v1/plugins/"+url.PathEscape(positional[0])+"/-/", nil, &d); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(d, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return nil
	case "install", "upgrade":
		upgrade, source := "", ""
		if args[0] == "upgrade" {
			if err := need(2); err != nil {
				return err
			}
			upgrade, source = positional[0], positional[1]
		} else {
			if err := need(1); err != nil {
				return err
			}
			source = positional[0]
		}
		var pv plugin.Preview
		if err := c.do(ctx, "POST", "/api/v1/plugins/-/preview", map[string]string{"source": source, "upgrade": upgrade}, &pv); err != nil {
			return err
		}
		printPreview(stdout, &pv)
		if !flag("--yes") && !confirm(stdin, stdout) {
			return errors.New("没有装")
		}
		var d PluginDetail
		body := map[string]string{"preview_id": pv.ID, "permissions_digest": pv.Digest}
		path := "/api/v1/plugins/-/install"
		if upgrade != "" {
			path = "/api/v1/plugins/" + url.PathEscape(upgrade) + "/-/upgrade"
		}
		if err := c.do(ctx, "POST", path, body, &d); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s %s：%s\n", d.ID, d.Version, d.Status)
		if len(d.NeedsSetup) > 0 {
			fmt.Fprintf(stdout, "还缺：%s（App 的 设置 › 插件，或 personad plugin config / secret）\n", strings.Join(d.NeedsSetup, "、"))
		}
		return nil
	case "rollback":
		if err := need(1); err != nil {
			return err
		}
		var d PluginDetail
		if err := c.do(ctx, "POST", "/api/v1/plugins/"+url.PathEscape(positional[0])+"/-/rollback", map[string]string{}, &d); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s 回到了 %s\n", d.ID, d.Version)
		return nil
	case "enable", "disable":
		if err := need(1); err != nil {
			return err
		}
		var d PluginDetail
		if err := c.do(ctx, "PUT", "/api/v1/plugins/"+url.PathEscape(positional[0])+"/-/enabled", map[string]bool{"enabled": args[0] == "enable"}, &d); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s：%s\n", d.ID, d.Status)
		return nil
	case "config":
		if err := need(2); err != nil {
			return err
		}
		id := positional[0]
		var d PluginDetail
		if err := c.do(ctx, "GET", "/api/v1/plugins/"+url.PathEscape(id)+"/-/", nil, &d); err != nil {
			return err
		}
		cfg := map[string]json.RawMessage{}
		_ = json.Unmarshal(d.Config, &cfg)
		for _, kv := range positional[1:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return fmt.Errorf("%q is not key=value", kv)
			}
			if v == "" {
				delete(cfg, k)
				continue
			}
			if json.Valid([]byte(v)) {
				cfg[k] = json.RawMessage(v)
			} else {
				cfg[k], _ = json.Marshal(v)
			}
		}
		if err := c.do(ctx, "PUT", "/api/v1/plugins/"+url.PathEscape(id)+"/-/config", map[string]any{"config": cfg}, &d); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s：%s\n", d.ID, d.Status)
		return nil
	case "secret":
		if err := need(2); err != nil {
			return err
		}
		path := "/api/v1/plugins/" + url.PathEscape(positional[0]) + "/-/secrets/" + url.PathEscape(positional[1])
		var d PluginDetail
		if flag("--delete") {
			if err := c.do(ctx, "DELETE", path, nil, &d); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s：删掉了 %s\n", d.ID, positional[1])
			return nil
		}
		value, err := readSecret(stdin, stdout)
		if err != nil {
			return err
		}
		if err := c.do(ctx, "PUT", path, map[string]string{"value": value}, &d); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s：%s\n", d.ID, d.Status)
		return nil
	case "uninstall":
		if err := need(1); err != nil {
			return err
		}
		path := "/api/v1/plugins/" + url.PathEscape(positional[0]) + "/-/"
		if flag("--keep-data") {
			path += "?keep_data=1"
		}
		if err := c.do(ctx, "DELETE", path, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "卸载了 %s\n", positional[0])
		return nil
	}
	return fmt.Errorf("unknown plugin command %q\n%s", args[0], pluginUsage)
}

func printPreview(w io.Writer, pv *plugin.Preview) {
	m := pv.Manifest
	if pv.Upgrade {
		fmt.Fprintf(w, "升级 %s：%s → %s\n", m.ID, pv.From, m.Version)
	} else {
		fmt.Fprintf(w, "安装 %s %s（%s）\n", m.ID, m.Version, m.Name)
	}
	if m.Publisher != "" {
		fmt.Fprintf(w, "发布者：%s\n", m.Publisher)
	}
	if m.Description != "" {
		fmt.Fprintln(w, m.Description)
	}
	for point, ids := range m.Contributions() {
		fmt.Fprintf(w, "提供 %s：%s\n", point, strings.Join(ids, "、"))
	}
	fmt.Fprintln(w, "它可以：")
	if len(pv.Permissions) == 0 {
		fmt.Fprintln(w, "  （不运行程序，只有声明）")
	}
	for _, l := range pv.Permissions {
		fmt.Fprintf(w, "  · %s：%s\n", l.Title, l.Detail)
	}
	if pv.MorePermissions {
		fmt.Fprintln(w, "新版本要的权限比现在多。")
	}
	for _, warn := range pv.Warnings {
		fmt.Fprintf(w, "注意：%s\n", warn)
	}
}

func confirm(stdin io.Reader, stdout io.Writer) bool {
	fmt.Fprint(stdout, "继续吗？[y/N] ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// readSecret reads a secret from the terminal without echo, or the first line of a pipe.
func readSecret(stdin io.Reader, stdout io.Writer) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stdout, "值（不显示）：")
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// localHubConfig is hub.json on this machine (the token, the port).
func localHubConfig() (*Config, error) {
	dir := dataDir()
	cfg := &Config{Listen: ":18660", dir: dir}
	b, err := os.ReadFile(dir + "/hub.json")
	if err != nil {
		return nil, fmt.Errorf("这台机器上没有 hub 的配置（%s/hub.json）：插件只能在 hub 机器上管理", dir)
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	cfg.applyEnv()
	return cfg, nil
}

type pluginClient struct{ cfg *Config }

// do calls the hub on this machine over loopback with the master token.
func (c *pluginClient) do(ctx context.Context, method, path string, body, out any) error {
	_, port, err := net.SplitHostPort(c.cfg.Listen)
	if n, perr := strconv.Atoi(port); err != nil || perr != nil || n <= 0 || n > 65535 {
		return errors.New("hub.json listen has no valid port")
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+net.JoinHostPort("127.0.0.1", port)+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 10 * time.Minute, Transport: transport} // a download may take a while
	res, err := client.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			return errors.New("hub 没在运行：先启动 hub")
		}
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("%s（HTTP %d）", e.Error, res.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
