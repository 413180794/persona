// Package manager is `personad manager …`: a manager made in the app (docs/managers.md, "A second manager") joins the hub
// with its one-time install code, runs, and is installed as a service of its own (systemd user unit, launchd agent,
// or a container). The manager itself is the assistant's program (assistant.RunManager) with a directory of its own.
package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/update"
)

// Usage is the help of `personad manager`.
const Usage = `usage:
  personad manager join <hub 地址> <安装码> [--dir 目录] [--database-url URL] [--fingerprint 指纹] [--install]
                       用 App 里「新建管理者」给的安装码换这个管理者的 token，写进目录里的 manager.json
                       （默认 ~/.persona/managers/<id>）；数据库是它自己的 Postgres：install 时在 Docker 里建
                       （容器 persona-mgr-<id>-pg），或者 --database-url 给一个
  personad manager run [--dir 目录] [--join <hub 地址> <安装码>]
                       运行这个管理者（--join：目录里还没有 manager.json 时先换码，容器用）
  personad manager install [--dir 目录]     建好它自己的 Postgres（Docker），装成常驻服务（Linux：systemd 用户服务；Mac：launchd）
  personad manager uninstall [--dir 目录] [--purge]
                       停掉并去掉常驻服务；它的数据库容器停掉、数据卷留着（--purge：连容器和数据卷一起删掉）
  personad manager status [--dir 目录]      这台电脑上的管理者和它们的服务
`

// errUsage: the arguments are wrong; the caller prints Usage.
var errUsage = errors.New("arguments")

// ErrUsage is errUsage for the command line (personad prints Usage and exits 2).
var ErrUsage = errUsage

// Main runs `personad manager <args>`.
func Main(ctx context.Context, build update.Build, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	e := newEnv(stdout)
	switch args[0] {
	case "join":
		return cmdJoin(ctx, e, args[1:])
	case "run":
		return cmdRun(ctx, e, build, args[1:])
	case "install":
		return cmdInstall(ctx, e, args[1:])
	case "uninstall":
		return cmdUninstall(ctx, e, args[1:])
	case "status":
		return cmdStatus(ctx, e, args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(stdout, Usage)
		return nil
	}
	return errUsage
}

// --- join ---------------------------------------------------------------------------------------------------------

// JoinOptions are `personad manager join`'s.
type JoinOptions struct {
	Hub, Code   string
	Dir         string // "" → assistant.DefaultManagerDir(id)
	DatabaseURL string // "" → its own Postgres in Docker, made by install (Docker must be there: checked first)
	Fingerprint string // the hub's TLS pin, for an https hub with its own certificate
	// CheckDocker says why Docker can't make its database (nil: it can); nil checks this machine.
	CheckDocker func(ctx context.Context) error
	// Warn gets what the user should know beyond the join itself (a database container left unused); nil: nobody.
	Warn       io.Writer
	HTTPClient *http.Client // tests
}

// joinReply is POST /api/v1/bus/join's answer (hub/managers.go).
type joinReply struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
	HubID string `json:"hub_id"`
}

// Join trades the install code for the manager's token and writes manager.json; it returns the directory. What it
// can check before the code is spent (that there will be a database: Docker to make it, or --database-url) it checks
// first: a code works once.
func Join(ctx context.Context, o JoinOptions) (string, *assistant.ManagerConfig, error) {
	hub := strings.TrimRight(strings.TrimSpace(o.Hub), "/")
	if !strings.HasPrefix(hub, "http://") && !strings.HasPrefix(hub, "https://") {
		return "", nil, fmt.Errorf("hub 地址 %q 要写成 http(s)://主机:端口", o.Hub)
	}
	code := strings.TrimSpace(o.Code)
	if code == "" {
		return "", nil, errors.New("没有安装码：在 App 的「设置 › 管理者 › 新建」里拿一个")
	}
	if o.DatabaseURL != "" {
		if _, err := db.WithDatabase(o.DatabaseURL, "x"); err != nil {
			return "", nil, ConfigError{fmt.Errorf("--database-url: %w", err)}
		}
		if hub := assistant.HubDatabaseURL(); hub != "" && assistant.SameDatabase(o.DatabaseURL, hub) {
			return "", nil, ConfigError{assistant.ErrHubDatabase}
		}
	} else if !hasDatabase(o.Dir) { // joined again into its directory (after a revoke): its database is there
		check := o.CheckDocker
		if check == nil {
			check = newEnv(io.Discard).dockerProblem
		}
		if err := check(ctx); err != nil {
			return "", nil, err // before the code is spent: it works once
		}
	}
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
		if o.Fingerprint != "" {
			client = link.Pinned(o.Fingerprint)
			client.Timeout = 30 * time.Second
		}
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hub+bus.JoinPath, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("连不上 hub %s：%w", hub, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return "", nil, errors.New("hub 不认这个安装码：它不对、过期了（10 分钟）或者已经用过；在 App 里给这个管理者重新拿一个")
	case http.StatusTooManyRequests:
		return "", nil, errors.New("试错太多次，hub 暂时不接受这台电脑换码：过几分钟再试")
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return "", nil, fmt.Errorf("hub %s 还不支持管理者（HTTP %d）：先升级 hub", hub, resp.StatusCode)
	default:
		return "", nil, fmt.Errorf("hub 没换成（HTTP %d）：%s", resp.StatusCode, firstLine(string(raw)))
	}
	var j joinReply
	if err := json.Unmarshal(raw, &j); err != nil || j.ID == "" || j.Token == "" {
		return "", nil, errors.New("hub 的回答不对：没有 id 或 token")
	}
	c := assistant.ManagerConfig{ID: j.ID, Name: j.Name, Hub: hub, HubID: j.HubID, Token: j.Token,
		Fingerprint: o.Fingerprint, DatabaseURL: o.DatabaseURL}
	dir := o.Dir
	if dir == "" {
		dir = assistant.DefaultManagerDir(j.ID)
	}
	if old, err := assistant.ReadManagerConfig(dir); err == nil && old.ID != j.ID {
		// The directory is another manager's: keep both, this one in its own.
		dir = assistant.DefaultManagerDir(j.ID)
	} else if err == nil && c.DatabaseURL == "" {
		// Installed again in the same place: the same database.
		c.DatabaseURL, c.DatabaseContainer = old.DatabaseURL, old.DatabaseContainer
	} else if err == nil && old.DatabaseContainer != "" && o.Warn != nil {
		// Another database given now: the container install made is not used any more (nor removed).
		fmt.Fprintf(o.Warn, "它原来的数据库容器 %s（数据卷 %s）不再用了，留着没删：不要了就 docker rm -f %s && docker volume rm %s\n",
			old.DatabaseContainer, dbVolume(c.ID), old.DatabaseContainer, dbVolume(c.ID))
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		return "", nil, fmt.Errorf("写不了 %s：%w（token 已经换出来了：在 App 里再拿一个安装码重来）", filepath.Join(dir, assistant.ManagerFile), err)
	}
	return dir, &c, nil
}

// hasDatabase: dir has a manager.json that names its database.
func hasDatabase(dir string) bool {
	if dir == "" {
		return false
	}
	c, err := assistant.ReadManagerConfig(dir)
	return err == nil && c.DatabaseURL != ""
}

// databaseEnv is how the container `commands.docker` starts hands the manager its database (the address and its
// password): in the environment, never on a command line (`ps`, `docker inspect`'s Cmd). Not PERSONA_DATABASE_URL:
// that is the hub's (HubDatabaseURL), which a manager refuses.
const databaseEnv = "PERSONA_MANAGER_DATABASE_URL"

func cmdJoin(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("manager join", flag.ContinueOnError)
	fs.SetOutput(e.stdout)
	dir := fs.String("dir", "", "管理者的目录（默认 ~/.persona/managers/<id>）")
	dbURL := fs.String("database-url", "", "它的 Postgres 库（默认 install 时在 Docker 里给它建一个）")
	fp := fs.String("fingerprint", "", "hub 的 TLS 指纹（https 地址用 hub 自己的证书时）")
	install := fs.Bool("install", false, "换好以后接着装成常驻服务（manager install）")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errUsage
	}
	d, c, err := Join(ctx, JoinOptions{Hub: pos[0], Code: pos[1], Dir: *dir, DatabaseURL: *dbURL, Fingerprint: *fp,
		CheckDocker: e.dockerProblem, Warn: e.stdout})
	if err != nil {
		return err
	}
	where := "它自己的 Postgres，install 时在 Docker 里建（容器 " + dbContainer(c.ID) + "）"
	if c.DatabaseContainer == "" && c.DatabaseURL != "" {
		where = db.DatabaseName(c.DatabaseURL) + "（--database-url 给的，没有就建）"
	}
	fmt.Fprintf(e.stdout, "加入了 hub：这台是管理者「%s」（%s）。\n  目录：%s\n  数据库：%s\n", c.Name, c.ID, d, where)
	if *install {
		return cmdInstall(ctx, e, []string{"--dir", d})
	}
	fmt.Fprintf(e.stdout, "下一步：%s manager install --dir %s（装成常驻服务），或者在前台 %s manager run --dir %s\n",
		e.self(), shellQuote(d), e.self(), shellQuote(d))
	return nil
}

// --- run ----------------------------------------------------------------------------------------------------------

// restart starts the program again once a new token is there (reexec; tests: nil, run again in this process).
var restart = reexec

// revokedPoll is how often a manager whose token was refused looks for a new one in manager.json.
var revokedPoll = 10 * time.Second

func cmdRun(ctx context.Context, e *env, build update.Build, args []string) error {
	fs := flag.NewFlagSet("manager run", flag.ContinueOnError)
	fs.SetOutput(e.stdout)
	dir := fs.String("dir", "", "管理者的目录（这台只有一个管理者时可以不写）")
	join := fs.Bool("join", false, "目录里还没有 manager.json 时，先用后面的 <hub 地址> <安装码> 换码")
	dbURL := fs.String("database-url", "", "和 --join 一起：它自己的 Postgres 库（容器里经环境变量 "+databaseEnv+" 给，不放命令行）")
	fp := fs.String("fingerprint", "", "和 --join 一起：hub 的 TLS 指纹")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if *join != (len(pos) == 2) || (!*join && len(pos) != 0) {
		return errUsage
	}
	d := *dir
	if *join && (d == "" || !fileExists(filepath.Join(d, assistant.ManagerFile))) {
		url := *dbURL
		if url == "" {
			url = e.getenv(databaseEnv)
		}
		if d, _, err = Join(ctx, JoinOptions{Hub: pos[0], Code: pos[1], Dir: d, DatabaseURL: url, Fingerprint: *fp,
			CheckDocker: e.dockerProblem, Warn: e.stdout}); err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "加入了 hub，目录 %s\n", d)
	}
	if d == "" {
		if d, err = onlyManager(); err != nil {
			return err
		}
	}
	return e.runManager(ctx, build, d)
}

// Run runs the manager in dir until ctx ends. When the hub refuses its token it says so and waits — not dialing —
// until manager.json has another one (`personad manager join` with a new code, into the same directory), so a
// service manager that restarts it does not turn into a loop of refused connections.
func Run(ctx context.Context, build update.Build, dir string, out io.Writer) error {
	return newEnv(out).runManager(ctx, build, dir)
}

func (e *env) runManager(ctx context.Context, build update.Build, dir string) error {
	out := e.stdout
	env := os.Environ() // as it started: RunManager points PERSONA_HOME at dir
	c, err := assistant.ReadManagerConfig(dir)
	if err != nil {
		return ConfigError{err} // restarting cannot fix it (the service's unit does not, exit 2)
	}
	// Its own Postgres comes up with it (started again after a reboot; made here when install did not).
	if _, err := e.ownDatabase(ctx, dir, c); err != nil {
		return err
	}
	for {
		before, _ := assistant.ReadManagerConfig(dir)
		err := assistant.RunManager(ctx, build, dir)
		if !errors.Is(err, bus.ErrRevoked) {
			return err
		}
		id := ""
		if before != nil {
			id = before.ID
		}
		fmt.Fprintf(out, "manager: [%s] hub 不再认这个管理者的 token（在 App 里删掉了，或者在别处重新装了）：不再连 hub。"+
			"要接着用这个目录：在 App 里给它拿一个新安装码，运行 personad manager join <hub 地址> <安装码> --dir %s\n", id, shellQuote(dir))
		if err := waitNewToken(ctx, dir, before); err != nil {
			return err
		}
		fmt.Fprintf(out, "manager: [%s] manager.json 里有了新 token，重新连 hub\n", id)
		if restart != nil {
			if err := restart(env); err != nil {
				fmt.Fprintf(out, "manager: [%s] could not start again in place (%v): running it again here\n", id, err)
			}
		}
	}
}

// waitNewToken returns once manager.json in dir has a token other than before's, or with ctx's error.
func waitNewToken(ctx context.Context, dir string, before *assistant.ManagerConfig) error {
	t := time.NewTicker(revokedPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		c, err := assistant.ReadManagerConfig(dir)
		if err == nil && (before == nil || c.Token != before.Token) {
			return nil
		}
	}
}

// managersRoot is where the managers of this machine are by default (~/.persona/managers).
func managersRoot() string { return filepath.Dir(assistant.DefaultManagerDir("x")) }

// localManagers are the directories under managersRoot with a manager.json, but the built-in manager's (the
// assistant the hub runs, managerconf.DefaultDir: its manager.json holds only its configuration).
func localManagers() []string {
	entries, _ := os.ReadDir(managersRoot())
	var out []string
	for _, ent := range entries {
		d := filepath.Join(managersRoot(), ent.Name())
		if ent.Name() == assistantwire.AssistantID {
			continue
		}
		if ent.IsDir() && fileExists(filepath.Join(d, assistant.ManagerFile)) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// onlyManager is the one manager of this machine, for a command without --dir.
func onlyManager() (string, error) {
	list := localManagers()
	switch len(list) {
	case 1:
		return list[0], nil
	case 0:
		return "", fmt.Errorf("%s 下没有管理者：先 personad manager join，或者用 --dir 指定目录", managersRoot())
	}
	return "", fmt.Errorf("这台有 %d 个管理者（%s）：用 --dir 指定是哪个", len(list), strings.Join(list, "、"))
}

// --- helpers ------------------------------------------------------------------------------------------------------

// parseInterleaved parses flags that may come before, between or after the positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, errHelp
			}
			return nil, errUsage
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// ConfigError is a mistake in the manager's configuration (no manager.json, no database to use): personad exits 2,
// which its service does not restart on.
type ConfigError struct{ Err error }

func (e ConfigError) Error() string { return e.Err.Error() }
func (e ConfigError) Unwrap() error { return e.Err }

// errHelp: -h was asked for; the flags' help is printed already.
var errHelp = errors.New("help")

// ErrHelp is errHelp for the command line (exit 0).
var ErrHelp = errHelp

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// shellQuote makes s one word for sh.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=:@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// dataDir is this machine's data directory (~/.persona), for the binary the service runs.
func dataDir() string { return identity.HomeDir() }
