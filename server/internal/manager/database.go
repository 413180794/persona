package manager

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/pgdocker"
)

// A manager's own Postgres (docs/assistant/managers.md, "Database"): a Docker container and volume named after it, its port
// published on 127.0.0.1, made by `personad manager install` (or the first `manager run`) the way `personad hub setup`
// makes the hub's (internal/pgdocker). The password is generated here and kept only in manager.json (0600). It is
// never the hub's Postgres: the hub doesn't touch a manager's database. --database-url gives another one instead.

// dbContainer and dbVolume are manager id's database container and volume.
func dbContainer(id string) string { return pgdocker.ManagerContainer(id) }
func dbVolume(id string) string    { return pgdocker.ManagerVolume(id) }

// errNoDocker is what join, install and run say without Docker and without --database-url.
var errNoDocker = errors.New("这个管理者没有数据库：装上 Docker（它给管理者建一个自己的 Postgres 容器），或者加 --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'（安装码没有用掉）")

// dockerProblem is a configuration error (exit 2) saying why Docker can't make the database; nil when it can.
func (e *env) dockerProblem(ctx context.Context) error {
	switch err := pgdocker.CheckDocker(ctx, e.docker, e.look); {
	case errors.Is(err, pgdocker.ErrNoDocker):
		return ConfigError{errNoDocker}
	case errors.Is(err, pgdocker.ErrDockerDenied):
		return ConfigError{fmt.Errorf("有 Docker，但当前用户没有权限用它（管理者的 Postgres 要用）：运行 sudo usermod -aG docker %s，退出登录再登录后重来；或者加 --database-url", e.user)}
	case err != nil:
		return errors.New("有 Docker，但它没在运行：运行 sudo systemctl start docker（Mac 上打开 Docker Desktop 或 OrbStack），再重来")
	}
	return nil
}

// pg is manager id's database container.
func (e *env) pg(id string) *pgdocker.Container {
	return &pgdocker.Container{Name: dbContainer(id), Volume: dbVolume(id), Run: e.docker, Sleep: time.Sleep, Wait: e.dbWait}
}

// ownDatabase makes sure the manager in dir has its database: the one it was given (--database-url) as it is, else its
// container (pgdocker.ProvisionManagerDB: made the first time, started every time). manager.json keeps the address.
// made: the container is new.
func (e *env) ownDatabase(ctx context.Context, dir string, c *assistant.ManagerConfig) (made bool, err error) {
	if c.DatabaseURL != "" && c.DatabaseContainer == "" {
		if hub := assistant.HubDatabaseURL(); hub != "" && assistant.SameDatabase(c.DatabaseURL, hub) {
			return false, ConfigError{assistant.ErrHubDatabase}
		}
		return false, nil
	}
	if err := e.dockerProblem(ctx); err != nil {
		return false, err
	}
	password := ""
	if u, err := url.Parse(c.DatabaseURL); err == nil && c.DatabaseURL != "" {
		password, _ = u.User.Password()
	}
	// It had one and both the container and its volume are gone: what follows is a new, empty database.
	lost := c.DatabaseContainer != "" && !e.pg(c.ID).Exists(ctx) && !e.volumeExists(ctx, c.ID)
	dbURL, made, err := pgdocker.ProvisionManagerDB(ctx, c.ID, pgdocker.ManagerDBOptions{Password: password, Run: e.docker,
		LookPath: e.look, FreePort: e.freePort, Ping: e.ping, Wait: e.dbWait})
	if err != nil {
		return made, fmt.Errorf("它的数据库没准备好：%w（docker logs %s 看原因）", err, dbContainer(c.ID))
	}
	if made && lost {
		fmt.Fprintf(e.stdout, "manager: [%s] 它的数据库容器 %s 和数据卷 %s 都不在了：建了一个新的空库，以前的对话、设置、收件箱都不在里面\n",
			c.ID, dbContainer(c.ID), dbVolume(c.ID))
	}
	if dbURL != c.DatabaseURL || c.DatabaseContainer != dbContainer(c.ID) {
		c.DatabaseURL, c.DatabaseContainer = dbURL, dbContainer(c.ID)
		if err := assistant.WriteManagerConfig(dir, *c); err != nil {
			return made, err
		}
	}
	return made, nil
}

// volumeExists: manager id's database volume is there.
func (e *env) volumeExists(ctx context.Context, id string) bool {
	_, err := e.docker(ctx, nil, "docker", "volume", "inspect", dbVolume(id))
	return err == nil
}

// databaseLine says where the manager's database is, for install and status.
func databaseLine(c *assistant.ManagerConfig, made bool) string {
	switch {
	case c.DatabaseContainer != "" && made:
		return fmt.Sprintf("建好了它自己的 Postgres（Docker 容器 %s，数据卷 %s）", c.DatabaseContainer, dbVolume(c.ID))
	case c.DatabaseContainer != "":
		return fmt.Sprintf("它自己的 Postgres（Docker 容器 %s，数据卷 %s）", c.DatabaseContainer, dbVolume(c.ID))
	case c.DatabaseURL != "":
		return "--database-url 给的（" + redacted(c.DatabaseURL) + "）"
	}
	return "还没有（install 时建）"
}

// redacted is a database URL without its password.
func redacted(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "?"
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), "xxx")
	}
	return u.Redacted()
}

func pingPostgres(ctx context.Context, dbURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	return conn.Ping(ctx)
}
