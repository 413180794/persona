# 服务端 personad

`personad` 是新名称，`jarvisd` 继续作为兼容别名；新默认运行目录 `~/.persona`，旧 `~/.jarvis` 继续兼容。本文的 systemd / launchd、数据库 / 容器 / 数据卷及发布文件名保留现有身份，不随产品改名强行重建。迁移版本与旧版本的区别见 [配置与兼容说明](config.md#命名与兼容)。

Go，单一二进制，`server/cmd/personad`。模式见 `docs/architecture.md`，协议见 `docs/protocol.md`。

## 代码

| 包 | 内容 |
| --- | --- |
| `internal/proto` | 线上类型 |
| `internal/rpc` | hub↔node 的双向 JSON-lines RPC |
| `internal/transcript` | Claude Code JSONL → `proto.Item`，增量读取、分页 |
| `internal/herdr` | herdr 的 socket API 客户端（CLI 兜底）和事件订阅 |
| `internal/herdrcore` | herdr-core（Herdr 桌面版背后的 `hc`）的 JSON-RPC 客户端和事件订阅，见 `docs/plan-orca-support.md`「herdr-core 宿主」 |
| `internal/node` | node：会话列表、启动、发送、屏幕采样、hook / mcp 的 unix socket、runtime 的 `runtime.sock`（`runtimes.go`，`docs/runtimes.md`） |
| `internal/hub` | hub：HTTP / WebSocket、机器连接、交互队列、手机工具转发、APNs |
| `internal/netaddr` | 本机地址、Linux netlink 标志和地址变化通知；不证明公网可达 |
| `internal/db` | Postgres：连接池、内嵌 SQL 迁移（`migrations/NNNN_*.sql`）、各表读写 |
| `internal/mail` | IMAP/SMTP、加密账号存储、邮件 HTTP 接口和 MCP 工具（`docs/mail.md`） |
| `internal/mcpserver` | `personad mcp`：给 claude 的 stdio MCP server |
| `internal/apns` | APNs 客户端（.p8 key、provider token、发一条推送）：hub 直连和推送中继共用 |
| `internal/pushcrypto` | 推送正文用手机公钥加密（HPKE），`docs/push-relay.md` |
| `internal/relay` | `personad relay`：推送中继，发布方运行，`docs/push-relay.md` |
| `internal/tunnel` | tailcat：node 的 key 文件和端点、hub 的拨号、经 tailcat 收发新二进制（下面「tailcat 打洞」） |

## 和 herdr 的连接

- node 直连 herdr 的 unix socket（`$HERDR_SOCKET_PATH`，否则 `$XDG_CONFIG_HOME/herdr/herdr.sock`，默认 `~/.config/herdr/herdr.sock`）：
  每次调用新开一条连接，写一行 `{"id","method","params"}`，读一行回复（单行上限 64 MB，超时 10 秒）。herdr CLI 只是同一个 API 的
  薄客户端（每次还要先 ping 一次），结果结构完全一样；socket 连不上或 herdr 太旧不认识方法（`invalid_request` + `unknown variant`）时
  退回 CLI。方法名以 `herdr api schema --json` 为准（`pane run` 对应 `pane.send_input` + `keys:["Enter"]`；`pane.process_info` 必须传
  `pane_id`，传 target 会回答当前聚焦的窗格）。
- `events.subscribe` 一条长连接：生命周期事件（workspace / tab / pane 的增删改）加每个 Agent 窗格的 `pane.agent_status_changed`
  （herdr 只按窗格订阅状态）。事件让 `pollAgents` 取一次 snapshot：先等 50ms 合并一串事件，列表重建最多每秒一次；
  Agent 进入或离开 blocked（卡片来了 / 没了）不受每秒一次的限制，50ms 后就取（见下面「卡片的快车道」）。平时 10 秒兜底一次。
  事件流没连上，或有 TRAEX 窗格（它的状态在标题的转圈字符里，只变转圈不发 `pane.updated`）时仍每秒一次。
- 订阅的坑：同一条连接第二次 subscribe 会被静默忽略，窗格集合变了要带全集重开连接；herdr 会关掉跟不上的订阅者，`events_lost`
  不一定送得到，以连接关闭为准重连（指数退避到 30 秒），重连后先取一次 snapshot 补齐。推来的事件名 herdr 0.9 和订阅时一样用点
  （`pane.agent_status_changed`），更老的用下划线（`pane_agent_status_changed`），两种都认。
- 本机实测（herdr 0.9.3，7 个 Agent）：稳定时 30 秒内零事件；tab 改名到 `sessions.changed` 发出 80–140ms（原来最多 1 秒）。
  联调：`./scripts/test/test-herdr.sh`（独立的 herdr 会话，见 `docs/testing.md`「herdr 联调」）。

### 卡片的快车道（#751）

提问、授权、计划审批这些卡片，以及它们的出现、答掉、被替换、消失，每一跳都直通，不经过节流、合并或批处理；别的变化照旧节流。

| 一跳 | 怎么直通 |
| --- | --- |
| herdr 认出 Agent 在等你 | herdr 自己的事（实测 0–320 ms） |
| node 上报 hub | 进入 / 离开 blocked 的状态事件不等每秒一次的列表重建，50 ms 合并后就取 snapshot、发 `sessions.changed` |
| hub 建卡 | 没有 hook 的会话不等（有 hook 的等 1.5 s，见 `docs/protocol.md`「终端对话框」）；读屏幕、解析、`interaction` 事件立刻广播 |
| 推到 App | WebSocket 直接排进每个连接；APNs 立刻发；灵动岛「需要你」的变化不合并、不受 30 s 间隔限制 |
| App 画出来 | 收到就进模型；Agents 列表扣留时也立刻落「需要你」和提问（`landCritical`，`docs/ios-state.md`）；对话页直接读模型 |

实测（2026-11-03，隔离的 herdr + 真实的 Claude Code 2.1.317、没有 Jarvis 的 hook，node 和 hub 在同一进程里按 SSH 那样接起来，
`hub/cardlatency_live_test.go`）：

| | 修之前 | 修之后 |
| --- | --- | --- |
| 对话框出现在终端 → App 收到卡片 | 4.0–10.4 s（6 次） | 0–0.35 s（8 次；屏幕每 25 ms 读一次，误差在这个量级） |
| 其中 herdr 认出 → hub 知道 blocked | 2.2–8.9 s | 0–0.16 s |
| 其中 hub 建卡 | 1.5 s | 0 ms |
| 在电脑上答掉 → 卡片消失 | 8.5 s | 0.17–0.59 s |
| 答完一道题 → 下一道题的卡（同一个 AskUserQuestion 两道题） | 没有新卡 | 0.61–0.72 s（含按键） |

慢在哪：
- #611（合并 herdr 和 Herdr App 两种引擎）以后，叫醒 `pollAgents` 的那个 select 自己吃掉了 herdr 的事件标记，接着又按标记找「谁有变化」，
  找不到，这次就不取 snapshot，只剩 10 秒兜底；
- herdr 0.9 推来的事件名带点，node 只认下划线的，分不出哪个是状态事件；
- hub 给每张屏幕卡固定先等 1.5 s，等一张没有 hook 的会话永远不会来的 hook 卡。

量法：`HERDR_SOCKET_PATH=<隔离 herdr 的 socket> JARVIS_LIVE_CARD_PANE=<跑 claude 的窗格> go test -tags herdrlive -run TestLiveCardLatency ./internal/hub/`
（`JARVIS_LIVE_CARD_TWO=1`：一次问两道题；`JARVIS_LIVE_CARD_TRACE=1`：每一跳的时间线）。本机没有 Go 时在 hub 上交叉编译测试二进制再拷回来。
App 那一跳由 UI 测试守住：`ScreenTruthTests.testWhatNeedsTheUserShowsUnderTheFinger`（手指按在列表上也 1 s 内上屏）、
`LiveUpdateTests.testAScreenCardInTheOpenConversation`（对话页 1 s 内出现、在电脑上答掉后 1 s 内消失）。

## 和 herdr-core 的连接

每台机器的 node 只用一个引擎（`node/engine.go`）：herdr（默认）、herdr-core（`node/hostherdrcore.go`）或 Orca（`node/hostorca.go`，下面「和 Orca 的连接」），用户在 App 里按机器选，
hub 连上时用 `engine.use` 告诉 node（`docs/protocol.md`「引擎」）。用 herdr 时 node 和原来完全一样，不碰 herdr-core（只有 `node.info`
问一次它在不在）。选了 herdr-core 而它没开时，立刻答「没有终端」，不拨号、不记日志。socket 位置、方法、映射和第一期不支持的部分见
`docs/plan-orca-support.md`「herdr-core 宿主」。
联调：`go test -tags hclive -run HerdrCoreLive ./internal/node/ -v`（自己起一个独立的 herdr-core，再只读地列一遍默认实例）。

## 和 Orca 的连接

选了 Orca 的机器，node 经 Orca 桌面版 runtime 的 unix socket 驱动它的终端（客户端 `internal/orcart`，宿主 `node/hostorca.go`）：
读 `orca-runtime.json` 拿 socket 和 token（Orca 每次启动换 token，连不上或 `unauthorized` 时重读），先 `status.get` 查协议版本。
socket 上没有推送，node 每秒轮询一次 `terminal.list` + `worktree.ps`；窗格对应会话靠 claude 进程环境里的 `ORCA_PANE_KEY`。
Orca 没开时立刻答「没有终端」，不记日志。映射和第一期不支持的部分见 `docs/plan-orca-support.md`「Orca 来源」。
联调（只读）：`go test -tags orcalive -run OrcaLive ./internal/node/ -v`。

## 部署

hub 有两种部署方式，功能一样：**原生二进制**（下面，`systemd --user` 服务 + Postgres 容器）和 **Docker**（hub + Postgres 两个容器，
适合 NAS 或只当 hub 的机器，`docs/docker.md`）。各台电脑上的 node 不管 hub 怎么部署，都是 `~/.persona/bin/personad`。

```sh
./scripts/deploy/deploy.sh                    # 编译 linux/amd64 + darwin/arm64，装到所有机器，重启 hub
./scripts/deploy/deploy.sh <机器 id> <机器 id> # 只装部分机器
```

- 二进制：每台机器 `~/.persona/bin/personad`。node 由 hub 经 `ssh <别名> ~/.persona/bin/personad node --stdio` 拉起，Mac 上不用常驻服务。
- hub：hub 机器上的 `systemd --user` 服务 `jarvisd`，HTTP `listen` 默认 `:18660`，另用 `public.tls_listen` 开启 TLS（如 `:18662`）。公网只放行 TLS 端口，IPv6 入站、主机防火墙和 TUN 回包排查见 [网络文档](network.md)。`journalctl --user -u jarvisd -f` 看日志。
  Docker 部署的 hub 不归 `deploy.sh` 管（它的配置里别把 hub 机器写进 `machines`，或只部署各台电脑：`deploy.sh <机器 id>`），
  升级是拉新镜像；「重新部署」用镜像里带的 node 构建。
- 换了 node 二进制后 hub 不会自动重连旧连接；`deploy.sh <hub 机器 id>` 会重启 hub，或在 App「机器」页点「重新连接」。
- 管理界面（`<hub 地址>/admin`，[admin.md](admin.md)）的访问范围：`deploy.sh --admin-access <loopback|lan|cidrs|any>`（`cidrs` 加
  `--admin-cidrs a,b`）设到 hub 上；不给时，hub 上从没选过就在终端里问，没终端用默认的 `lan` 并打印一行提醒。之后在 hub 机器上
  `personad hub admin access` 看 / 改，`personad hub admin reset-password` 重设管理员密码；在 hub 机器本机上做首次设置要的一次性设置码在
  hub 日志里（`admin: first-run setup code …`），或 `personad hub admin setup-code` 现取（[admin.md](admin.md)「首次设置」）。

### 轻量 hub 与部署前检查

轻量 hub（例如 2 核 / 2 GB）可以只运行 hub 和 Postgres，把 Agent 放在其他执行机器上。初次部署尚未接入执行机器时，
在 `hub.json` 显式写 `"machines": []`，避免省略后加载默认本机 node；`inbox.agent.enabled` 和 `kb.enabled` 可先设为 false，
接入数据源后再按需开启。以上是部署方式建议，尚无硬件最低配置的压测结论。

Linux 的 systemd 用户服务要在退出 SSH 后持续运行，需由管理员为部署用户执行 `loginctl enable-linger <用户>`。
安装 Docker 后先验证 `postgres:18-alpine` 能拉取；系统软件源可访问不代表 Docker Hub 可访问。拉取失败时不要把数据库初始化
或 hub 部署标记为完成，也不要输出 `hub.json` 中的 token 和数据库连接串。
国内网络若无法访问 Docker Hub，可尝试 AWS 公共仓库的 Docker 官方镜像
`public.ecr.aws/docker/library/postgres:18-alpine`，拉取成功后重新标记为 `postgres:18-alpine` 再运行数据库脚本。

公网入口使用独立域名的 HTTPS 反向代理，hub 和数据库只监听回环地址。Nginx 要转发 WebSocket 的 Upgrade / Connection，
关闭响应缓冲并延长读取超时；访问日志不要记录带 token 的查询字符串。重载 Nginx 后立即请求可能仍命中旧 worker，
应等待新配置生效后再验证证书、鉴权接口和 WSS 101。通过云控制台申请的免费证书需要在到期前重新申请并安装，
不要将已有 certbot 定时器视为这类证书的自动续期。

Windows PowerShell 通过管道传 shell 脚本时，末尾可能额外带 CR，导致远端报 `$'\r': command not found`；
使用 UTF-8 文件上传，或先编码为 base64 再在远端解码执行，避免依赖管道的换行编码。

## NAS（docker）部署与升级（兼容部署示例）

本节记录已有部署的操作：`~/.jarvis`、`jarvisd`、容器 / 镜像名和备份文件名按旧兼容身份保留，不是新装的默认名称。升级 / 回退要使用实际部署的路径和服务名，不应把这些命令全文替换后直接运行。

另一种装法是 `deploy/docker` 的 compose（[docker.md](docker.md)）；这里记的是现在这台 NAS 上手工跑的两个容器。

Synology DSM 上 `deploy.sh` 用不了（没有 `systemd --user`，docker 要 sudo），hub 和 Postgres 都跑在 docker 里。
`<nas>` 是开发机 SSH 到 NAS 的别名，`<内网地址>` 是 NAS 的内网 IPv4，`<提交>` 是要部署的提交；NAS 上的命令都以运行 hub 的用户执行（需要免密 sudo）。

DSM 上的坑：docker 要写全路径 `sudo /usr/local/bin/docker`（非交互 SSH 的 PATH 里没有）；`/tmp` 是 noexec，二进制别放那里；
系统自带的 `pg_dump` 是 11 版，导不了 18 的库，备份在容器里做；没开 SFTP 时 `scp` 会失败，用 `ssh … 'cat > 文件'` 上传。

### 构建并上传（开发机，在仓库里）

```sh
git worktree add --detach /tmp/jarvis-build <提交>
(cd /tmp/jarvis-build/server && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --always)-$(date +%Y%m%d%H%M)" -o /tmp/jarvisd-linux-amd64 ./cmd/personad)
git worktree remove /tmp/jarvis-build
ssh <nas> 'umask 077; mkdir -p ~/.jarvis/bin && cat > ~/.jarvis/bin/jarvisd.new && chmod 755 ~/.jarvis/bin/jarvisd.new' < /tmp/jarvisd-linux-amd64
```

### 首次部署（NAS 上）

1. Postgres：容器 `jarvis-postgres`，数据卷 `jarvis-pgdata`，只发布到 `127.0.0.1:18661`。建容器用的密码只管初始化，
   真正的密码另外随机生成，用 `ALTER ROLE` 设置，只写进 `hub.json`。

   ```sh
   D="sudo /usr/local/bin/docker"
   $D volume create jarvis-pgdata
   $D run -d --name jarvis-postgres --restart unless-stopped -p 127.0.0.1:18661:5432 \
     -v jarvis-pgdata:/var/lib/postgresql -e POSTGRES_USER=jarvis -e POSTGRES_DB=jarvis \
     -e POSTGRES_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_hex(24))')" \
     --log-driver json-file --log-opt max-size=20m --log-opt max-file=3 postgres:18-alpine
   until $D exec jarvis-postgres pg_isready -h 127.0.0.1 -U jarvis -d jarvis >/dev/null; do sleep 1; done
   export PW=$(python3 -c 'import secrets;print(secrets.token_hex(24))')
   echo "ALTER ROLE jarvis WITH PASSWORD '$PW';" | $D exec -i jarvis-postgres psql -U jarvis -d jarvis -q
   ```

2. `~/.jarvis/hub.json`（0600，`machines` 先留空，以后按 `docs/config.md` 补）：

   ```sh
   python3 - <<'PY'
   import json, os, secrets
   cfg = {
       "listen": ":18660",
       "token": secrets.token_hex(24),
       "public_url": "http://<内网地址>:18660",
       "hook_timeout_minutes": 30,
       "database_url": "postgres://jarvis:%s@127.0.0.1:18661/jarvis?sslmode=disable" % os.environ["PW"],
       "machines": [],
       "public": {"tls_listen": ":18662", "advertise_ipv6": True, "advertise_lan": True},
       "security": {"trusted_cidrs": [], "allow_public_plaintext": False},
   }
   fd = os.open(os.path.expanduser("~/.jarvis/hub.json"), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
   with open(fd, "w") as f:
       json.dump(cfg, f, indent=2)
   PY
   unset PW
   ```

3. 运行镜像 `jarvisd-runtime:alpine3.22`：不含 jarvisd（二进制从 `~/.jarvis/bin` 挂进去），带 ssh 客户端（hub 经 ssh 连各台电脑），
   以及 `netcat-openbsd`（经 SOCKS 代理连电脑时给 ssh 的 ProxyCommand 用，见下面第 7 步；busybox 的 `nc` 不支持代理）。
   `~/.jarvis/docker/Dockerfile`：

   ```dockerfile
   FROM alpine:3.22
   ARG UID
   ARG GID=100
   ARG USER_NAME
   ARG HOME_DIR
   RUN apk add --no-cache openssh-client ca-certificates tzdata netcat-openbsd \
    && adduser -D -H -u "$UID" -G users -h "$HOME_DIR" -s /bin/sh "$USER_NAME" \
    && mkdir -p "$HOME_DIR" && chown "$UID:$GID" "$HOME_DIR"
   ENV HOME=$HOME_DIR
   WORKDIR $HOME_DIR
   USER $UID:$GID
   ENTRYPOINT ["/opt/jarvis/bin/jarvisd"]
   CMD ["hub"]
   ```

   ```sh
   cd ~/.jarvis/docker && $D build --network host --build-arg UID=$(id -u) --build-arg GID=$(id -g) \
     --build-arg USER_NAME=$(id -un) --build-arg HOME_DIR=$HOME -t jarvisd-runtime:alpine3.22 .
   ```

4. 容器 `jarvisd`（这条命令存成 `~/.jarvis/docker/run-jarvisd.sh`，重建容器时再跑）：host 网络、`--init`、本用户的 uid；
   `~/.jarvis` 读写挂载，`~/.jarvis/bin` 只读挂到 `/opt/jarvis/bin`，`~/.ssh` 挂进去。容器里的 HOME 和 NAS 上一样。

   ```sh
   mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd
   REAL_HOME=$(readlink -f "$HOME")
   $D run -d --name jarvisd --restart unless-stopped --network host --init \
     --user "$(id -u):$(id -g)" -e TZ="$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')" \
     --log-driver json-file --log-opt max-size=20m --log-opt max-file=5 \
     -v "$REAL_HOME/.jarvis:$HOME/.jarvis" -v "$REAL_HOME/.jarvis/bin:/opt/jarvis/bin:ro" \
     -v "$REAL_HOME/.ssh:$HOME/.ssh" \
     jarvisd-runtime:alpine3.22 hub
   ```

5. 检查：

   ```sh
   $D exec jarvis-postgres psql -U jarvis -d jarvis -tAc "select max(version), count(*) from schema_migrations"
   curl -s http://127.0.0.1:18660/api/v1/health    # ok、version、fp
   openssl s_client -connect 127.0.0.1:18662 </dev/null 2>/dev/null | openssl x509 -pubkey -noout |
     openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '='   # 应等于 fp
   (umask 077; $D exec jarvisd /opt/jarvis/bin/jarvisd hub pair > ~/.jarvis/pair-$(date +%Y%m%d-%H%M%S).txt)  # 含 token，10 分钟内有效
   ```

6. 公网直连：DSM 防火墙放行 `18662/tcp`（`18660` 不放行），路由器放行 IPv6 入站，见 [网络文档](network.md)。

7. 只能经代理到达的电脑（比如公司内网里的开发机，NAS 上另有一个 SOCKS5 代理能进去）：照常用 `kind: ssh`，代理写在 NAS 的
   `~/.ssh/config` 里（容器挂的就是它），hub.json 只写别名。地址、账号和代理端口都是个人配置，只写在 NAS 上：

   ```
   Host <别名>
     HostName <电脑的内网地址>
     User <账号>
     HostKeyAlias <别名>
     IdentityFile ~/.ssh/<hub 专用密钥>
     IdentitiesOnly yes
     ProxyCommand nc -X 5 -x 127.0.0.1:<SOCKS 端口> %h %p
   ```

   - 这个 `nc` 是容器里的 netcat-openbsd；NAS 系统自带的 `nc` 未必认 `-X`，所以手动测试也在容器里跑，比如用同一个镜像起一个
     `--entrypoint sh` 的临时容器，执行和 hub 一样的 `ssh -T -o BatchMode=yes … <别名> '~/.jarvis/bin/jarvisd node --stdio'`。
   - hub 用 `BatchMode=yes`，主机密钥要先写进 `~/.ssh/known_hosts`。最好从一台已经可信地连过这台电脑的机器拿到它的公钥，
     按 `HostKeyAlias` 的别名写进去，不要首次连接就盲目接受。
   - 电脑那边的 `authorized_keys` 里，这把钥匙限制成只能启动 node：`restrict,command="~/.jarvis/bin/jarvisd node --stdio" ssh-ed25519 …`。
   - 改了 Dockerfile 要重建镜像和容器：先给旧镜像打一个带时间的标签留着回滚，再 build，然后 `$D stop jarvisd && $D rm jarvisd &&
     ~/.jarvis/docker/run-jarvisd.sh`。`docker restart` 用的还是旧镜像。

### 升级

按「构建并上传」把新二进制传成 `~/.jarvis/bin/jarvisd.new`，然后在 NAS 上：

```sh
~/.jarvis/docker/upgrade.sh ~/.jarvis/bin/jarvisd.new
```

脚本的步骤：

1. 确认新二进制能在 NAS 上跑（`version`）。
2. 停容器。
3. 把当前二进制复制成 `~/.jarvis/bin/jarvisd.bak-<时间>`。
4. 在容器里 `pg_dump -Fc` 导出到 `~/.jarvis/backups/jarvis-<时间>.dump`（0600）。停服后再导出，和旧二进制留下的状态一致。
5. 用 `mv` 换上新二进制，启动容器。
6. 等 `/api/v1/health`，核对版本号是新的、TLS 公钥指纹没变。
7. 打印最新三条迁移和启动以来的日志（隐藏已知的 lark-connector 报错）。
8. 打印对应的回滚命令。健康检查不过、版本不对或指纹变了时，退出码非零。

### 回滚

```sh
~/.jarvis/docker/rollback.sh --check ~/.jarvis/bin/jarvisd.bak-<时间> ~/.jarvis/backups/jarvis-<时间>.dump   # 只验证，不动 hub
~/.jarvis/docker/rollback.sh ~/.jarvis/bin/jarvisd.bak-<时间> ~/.jarvis/backups/jarvis-<时间>.dump
sudo /usr/local/bin/docker exec jarvis-postgres dropdb -U jarvis jarvis_before_rollback_<时间>   # 确认回滚没问题以后
```

脚本的步骤：

1. 把 dump 恢复进一个新库。恢复失败就什么都不改。
2. 停容器。
3. 原库改名为 `jarvis_before_rollback_<时间>` 保留，dump 之后的写入只在这个库里。
4. 新库改名为 `jarvis`。
5. 换回旧二进制，启动容器，等健康检查。

### 日志

```sh
sudo /usr/local/bin/docker logs -f jarvisd
sudo /usr/local/bin/docker restart jarvisd    # 改了 hub.json 以后
```

`hub.json` 没配 `lark` 时，日志里会一直有 `read lark-connector token …` 报错，不影响运行。要接 lark-connector，得先在 lark-connector 那边开启 agent API，
再在 `lark` 里填 `base_url` 和 `token`。hub 的容器里没有 docker CLI，读不了别的容器的环境变量。

## hub 连不进去的电脑（jarvis-link）

笔记本这类网络经常变的电脑，hub.json 里 kind 写 `link`：hub 不 ssh 它，等它上面的 jarvis-link（Mac 上是菜单栏 App「Persona Link」）
主动连上 TLS 端口，再在这条链路上开流连它的常驻 node（所以这台要装常驻 node，见下一节）。配对：`personad hub pair --machine <id>`，
NAS 上是 `sudo /usr/local/bin/docker exec jarvisd /opt/jarvis/bin/jarvisd hub pair --machine <id>`。全部细节见 [link.md](link.md)。

## hub 来连（kind service）

服务器、开发工作站这类地址固定、hub 连得到的电脑，hub.json 里 kind 写 `service`：电脑上的连接器（jarvis-link 的被动模式，`jarvis-link pair --listen`）
在本机开一个 TLS 端口，hub 按配对时的指纹钉扎拨过去，开流连常驻 node，不用 ssh（可选经 `socks5://` 代理）。在管理界面「电脑 › 添加电脑 ›
hub 来连」粘贴它打印的链接即可，加完立刻生效。凭据加密存 Postgres。细节见 [mesh.md](mesh.md)「kind service」和 [link.md](link.md)「被动模式」。
ssh 电脑照常能用，但不再推荐、管理界面不再添加，迁移见 [mesh.md](mesh.md)「SSH / tailcat：兼容」。

## hub 互联

推荐只装一台 hub，其他电脑装 node 服务。确实要几台 hub 时，配对后连成一组（`personad hub pair --peer` / `personad hub peer add`，或管理界面
「高级 › 互联」）：组里有且只有一台**中心**，服务所有 App、连所有电脑；其余是**成员**，把 App 的请求转给中心、把自己的电脑交给中心连
（`personad hub peer list` 看谁是中心，`personad hub peer center` 换中心）。同一台电脑只出现一次，用最好的那条路。互联只走 TLS 端口。
设计、协议和怎么验证见 [mesh.md](mesh.md)。

## 常驻 node（Mac，`scripts/deploy/node-launchd.sh`）

hub 通过 ssh 起 node（`personad node --stdio`），node 再起助手的 claude。ssh 会话读不了 macOS 的**登录钥匙串**，所以 API key 存在钥匙串里的
claude（`apiKeyHelper` 用 `security find-generic-password` 取）在助手里会报 401；herdr 里的 Agent 没这个问题，因为 herdr 跑在登录会话里。

- `scripts/deploy/node-launchd.sh install`（在那台 Mac 上跑）：装一个 launchd 用户任务 `com.jarvis.node`，登录后常驻 `personad node --attach`，
  监听 `~/.persona/node-attach.sock`。
- hub 照旧 ssh 起 `personad node --stdio`；它发现有常驻 node 在听，就只把 ssh 的输入输出转给它（`internal/node/attach.go` 的 `Relay`），
  由常驻 node 处理这条连接。node 起的东西（助手的 claude、它的 key helper）都在登录会话里，能读钥匙串。hub 不用改，也分不出区别。
- 没有常驻 node（没装、退出了、socket 是残留的）时 `--stdio` 自己处理连接，和以前一样。
- 一次只服务一条连接：新连接进来（hub 重连）时旧的那条的 node 退出，等着的 hook 会去问新的（同 ssh 起的 node 被替换时）。同时只能有一个常驻 node。
- `deploy.sh` 换了二进制后会 `node-launchd.sh restart`（ssh 进来也能操作登录会话的 launchd）；hub 要重连一次才会用上新的 node（重启 hub，或 App 里「重新连接」）。
- Agent 运行时的 `~/.persona/runtime.sock` 是常驻 node 进程的，不是某一条 hub 连接的（`node/runtimehost.go`）：常驻 node 一起来就开着，
  DSH 插件这类 runtime 不用等 hub；hub 换连接时它们不断开，新的连接一上来就列出它们的会话（`docs/protocol.md`「Runtime ↔ node」）。
- `scripts/deploy/node-launchd.sh uninstall` 回到 ssh 起 node。日志：`~/.persona/node-attach.log`。

## tailcat 打洞（`internal/tunnel`，`hub/tailcat.go`）

hub ssh 不进去的电脑（NAT 后面、另一个内网、没有公网 IP）经 [tailcat](https://github.com/tailscale/tailcat) 接入：Tailscale 的数据面
（WireGuard + DERP 中继 + 打洞），不用它的控制面，不需要 Tailscale 账号、root、TUN 或开端口，全在 personad 进程里（用户态 netstack）。

- **退避顺序**：配了 `ssh_host` 又配了 `tailcat_addr` 的机器每次连接先试 ssh（`ConnectTimeout=15`），失败立刻试 tailcat；
  ssh 失败后 5 分钟内的重连先试 tailcat，不再干等 ssh 超时。走 tailcat 期间每 5 分钟 `ssh <host> true` 检查一次，ssh 恢复就断开
  重连回 ssh（有提问 / 计划 / 授权在等手机时不切，重连会取消它们）；切回去又马上失败（ssh 能登录但 node 不回话，比如慢链路上
  40 秒内等不到 `node.info`）时，下一次检查的间隔翻倍，最多 32 倍，ssh 稳定 10 分钟后复原，不会每 5 分钟断一次。`kind: "tailcat"` 只走 tailcat。机器列表的 `transport`
  是当前走哪条，`tailcat_path` 是 tailcat 走的路径（`direct` 打洞成功 / `relay` 经中继，每分钟问一次，同时促使它重新找直连）。
- **断线检测**：tailcat 的用户态 TCP 没有 keepalive（ssh 有 `ServerAliveInterval`），对面睡眠 / 断网时连接不会自己断。hub 每分钟给
  node 发一次 `ping`，连续两次不回就断开重连（旧 node 回「unknown method」也算回了）。这一分钟里有数据进出（读到数据，或大的写
  每 32 KB 发出去一段）就算活着、不 ping：慢中继上一个大页面要传很久，ping 排在它后面会误判。ping 在单独的 goroutine 里等，
  卡在死连接的写上也不耽误判断；判定断线时直接关掉底层连接，卡住的写随之失败。
- **node 侧**：只有常驻 node（`scripts/deploy/node-launchd.sh install`，Mac 是 launchd，Linux 是 systemd 用户服务）会开 tailcat：
  有 `~/.persona/tailcat.json` 时，在自己的 tailcat 地址上听 `18662`（node 协议，和 ssh 转过来的连接共用「一次服务一条，新的替换旧的」）
  和 `18663`（接收新二进制）。起不来（没网、DERP 连不上）就在后台退避重试，ssh 转发照常服务。
- **配对**：hub 机器上 `personad hub tailcat-key` 打印 hub 的公钥（第一次生成 `~/.persona/tailcat-hub.json`）；那台电脑上
  `personad node tailcat init --hub <公钥>` 生成 `~/.persona/tailcat.json`（node 私钥、预共享密钥、选定的 DERP 区域、允许的 hub 公钥）
  并打印地址，填进 hub.json 的 `tailcat_addr`；重启那台的常驻 node 和 hub。再跑一次 `init` 可以加别的 hub，地址不变；
  `personad node tailcat addr` 再打印一遍。
- **安全**：node 只回应 `tailcat.json` 里列出的 hub 公钥（tailcat 在列表为空时放所有人进来，所以没有 hub 公钥的文件直接拒绝加载），
  其他 key 连握手都没有；只放行 18662–18663 两个端口。地址里有预共享密钥，和 hub token 一样是密钥：只放 hub.json，日志里不打印。
  两个 key 文件都是 0600（读到权限更宽的会改回 0600）。
- **吊销 / 换密钥**：node 只在启动时读允许的 hub 公钥，tailcat 也不会踢掉已经连上的 peer。去掉一个 hub：从那台的 `tailcat.json`
  的 `hubs` 删掉它，再 `scripts/deploy/node-launchd.sh restart`。地址泄露（换预共享密钥和 node 私钥）：删掉 `tailcat.json` 重新 `init`，
  把新地址填进 hub.json 并重启 hub。换 hub 的 key：删掉 hub 机器的 `tailcat-hub.json`，重启 hub 后用新的 `hub tailcat-key` 在每台重新 `init --hub`，
  再从各台 `tailcat.json` 的 `hubs` 删掉旧的、重启常驻 node。
- **地址固定**：地址只写 DERP 区域 id（不内嵌中继服务器），`init` 时选一次最近的区域存起来；否则每次启动重新选区域，地址会变。
- **部署**：走 tailcat 的机器点「重新部署」时，hub 把二进制经 tailcat 的 18663 发过去（一行 `{"size","sha256"}` + gzip）；node 校验
  大小和 sha256、确认新二进制在本机跑得起来（`version`）后才替换自己，回 `{"ok"}` 再退出，由 launchd / systemd 拉起新版本。
  一次只收一个（同时来的第二个立即回「正在上传」）；60 秒没收到数据就放弃，hub 中途重启不会把下一次部署卡住。
  `deploy.sh` 首次安装仍要能 ssh 到那台（或手动放二进制）。
- **代价**：personad 从约 19 MB 变成约 36 MB（gzip 后 8 MB → 15 MB）；冷启动约 7 秒（拉 DERP map、选区域），第一次拨通约 6.5 秒。
  公共中继没有 SLA、走中继时限速，可以用 `--derp-map-url` / hub.json 的 `tailcat.derp_map_url` 换成自建的 derper（见下一条）。tailcat 不承诺 API 稳定，
  升级依赖时跑一遍下面的联调。
- **自建中继（derper）**：内容本来就是 WireGuard 端到端加密的，但公共中继看得到哪两个 key 在通信、双方 IP 和流量大小；
  各台电脑和 hub 在同一个内网时，可以在 hub 所在的机器上跑一个只绑内网地址的 derper，找路和兜底转发都不出内网：
  1. 编译：`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go install tailscale.com/cmd/derper@<版本>`，版本取 `server/go.mod` 里 `tailscale.com` 那一行，和 tailcat 用的对齐。
  2. 证书：只有 IP、没有域名时用自签证书，客户端靠 DERP map 里节点的 `CertName: "sha256-raw:<证书 DER 的 sha256>"` 固定它，不需要 CA。
     自己用 openssl 生成一张长期的（`subjectAltName=IP:<内网 IP>`），放成 `<certdir>/<内网 IP>.crt` / `.key`；不要用 derper 缺证书时自动生成的那张（见 `docs/lessons/server.md`）。
  3. 运行（systemd 用户服务）：`derper -a <内网 IP>:<DERP 端口> -certmode manual -hostname <内网 IP> -certdir <certdir> -c <私钥文件> -http-port -1 -stun-port <STUN 端口> -home blank`。
     `-a` 带了 IP，STUN 也只绑这个 IP；`-c` 一定要给。
  4. DERP map：derper 不发 map，tailcat 是用 HTTP GET 取的，另起一个静态服务（例如 `python3 -I -m http.server <端口> --bind <内网 IP> --directory <目录>`）
     发 `derpmap.json`：一个区域（id 用 900–999，留给自定义区域的段），一个节点，`HostName` / `IPv4` 是内网 IP，`IPv6: "none"`，加上 `CertName`、`DERPPort`、`STUNPort`。
     发出去之前在每台电脑上取一次 map、用 `openssl s_client` 拿到证书算 sha256，和 `CertName` 对一下。
  5. 切换：hub.json 写 `tailcat.derp_map_url`；每台电脑跑 `personad node tailcat init --hub <公钥> --derp-map-url <map 地址>`。密钥不变，但会在新 map 里重新选区域，
     地址因此变了，要把新地址换进 hub.json 那台的 `tailcat_addr`，再重启那台的常驻 node 和 hub。改了 map（换证书、换端口）也要重启两边。
  6. 验证：机器列表的 `tailcat_path` 还是 `direct`；node 的日志是 `DERP region 9xx`；两边的 personad 只连到 derper 的端口，没有到公共中继的连接（`ss -tnp` / `lsof -i`）。
- **只管 hub ↔ node**：手机 / 网页到 hub 仍然需要一个能访问到的入口（`public_url`）。
- 联调（要外网）：`JARVIS_TEST_TAILCAT=1 go test ./internal/tunnel -run EndToEnd`（真实中继上的拨号、鉴权、上传），以及
  `internal/hub/tailcat_e2e_test.go` 顶部注释里的命令（两版真实 personad：ssh 不通 → 走 tailcat 上线 → 经 tailcat 部署新版本并重连）。
  `JARVIS_TAILCAT_DEBUG=1` 打开 tailscale 的日志（默认静音，打洞期间每秒好几行）。

## 数据库（Postgres）

hub 机器上的 docker 容器 `jarvis-postgres`（`postgres:18-alpine`，只监听 `127.0.0.1:18661`，数据卷 `jarvis-pgdata`，`--restart unless-stopped`）。
`scripts/deploy/db.sh` 幂等地创建 / 启动它，首次创建时生成密码写进 `hub.json` 的 `database_url`；`deploy.sh <hub 机器 id>` 每次都会先跑它。
hub 启动时自动执行未跑过的迁移（`schema_migrations` 记录，advisory lock 防并发）。

不想在 hub 机器上装 Docker 时，可以用系统自带的 Postgres（例如 Ubuntu 的 `postgresql` 包，16 版实测可用，只监听 localhost）：自己建用户 `jarvis`
和库 `jarvis`、`jarvis_test`（属主都是 `jarvis`，后者给 `test-server.sh` 用），把 `postgres://jarvis:<密码>@127.0.0.1:5432/jarvis?sslmode=disable`
写进 `hub.json` 的 `database_url`（0600）。hub 机器上没有 docker 时，`db.sh` 不建容器，只用 `pg_isready` 检查这个地址能不能连上，
`deploy.sh` 和 `test-server.sh` 照常用。下面「连库」那条命令里的 `docker exec … psql` 换成 `psql "<database_url>"`。

| 表 | 内容 |
| --- | --- |
| `live_activities` | 每台设备的实时活动：活动 token、push-to-start token、开始时间（`docs/live-activity.md`） |
| `devices` | 推送设备（APNs token），APNs 返回 410 / BadDeviceToken 时自动删除；`prefs`（jsonb）是这台设备要哪些推送（App 设置 › 通知），注册时不覆盖 |
| `phone_tools` | iPhone 最近一次声明的 phone_* 工具（手机离线时 Claude 也能看到） |
| `node_sessions` | 每台机器上 Persona 启动过的会话：pane、启动器、目录（node 经 hub RPC `state.load` / `state.put` 读写，Mac 不直连数据库） |
| `mail_accounts` | 邮箱账号：非敏感字段 jsonb + AES-GCM 密文；密钥 `~/.persona/secret.key` 不进数据库 |
| `interactions` | 每次提问 / 计划 / 授权 / 终端交互及结局（resolved / dismissed / expired / cancelled）；hub 重启时把残留的 pending 标成 cancelled |
| `settings` | App 里改的全局设置（如助手的启动器），以及持久化的 `hub_id` |
| `pair_tokens` | 临时配对 token 的哈希、到期和兑换记录，重启后仍有效 |

连库：`ssh <hub 机器> 'docker exec -it jarvis-postgres psql -U jarvis -d jarvis'`。
测试：`./scripts/test/test-server.sh`（同步代码到 hub 机器跑全部测试；本机没有 docker，DB 测试在本机会 skip）。`JARVIS_TEST_DATABASE_URL`
（脚本指向 `jarvis_test`）只是起点：每个测试二进制在它旁边建一个自己的库、跑完删掉（`server/internal/db/dbtest`），几处同时跑也互不干扰；
所以 `jarvis` 角色要能建库（容器里的是超级用户；自己装的 Postgres 给它 `CREATEDB`，不给就退回成排队用 `jarvis_test`）。

旧版的 `phone_tools.json`、`devices.json`、`mail_accounts.json`、`sessions.json` 首次启动时自动导入，改名为 `*.migrated`。

## 配置（hub 机器 `~/.persona/`，0600，不入库）

| 文件 | 内容 |
| --- | --- |
| `hub.json` | `listen`、`token`、`public_url`、`public`（TLS / 候选地址）、`security`（来源信任 / 鉴权限流，逐项见 [network.md](network.md#hubjson-网络配置)）、`name`（hub 的显示名）、`admin`（管理界面的访问范围，[admin.md](admin.md)）、`hook_timeout_minutes`、`database_url`、`machines`、`google.client_id`（Gmail 用 Google 账号登录的 iOS OAuth 客户端）、`google.proxy`（Gmail 流量走的 HTTP 代理，如 `http://127.0.0.1:<端口>`），见 `docs/mail.md`；`github.proxy`（GitHub 连接器的全部请求走的 HTTP 代理，同一格式，见 `docs/config.md`）；`notify`（推送时机，`docs/config.md`） |
| `tls/hub.key`、`tls/hub.crt` | 自动管理的 ECDSA P-256 身份密钥和自签证书；目录 0700，文件 0600，私钥不进数据库；换密钥后设备重新配对 |
| `secret.key` | 邮箱凭据的加密密钥（32 字节） |
| `apns.json`、`apns_key.p8` | 推送配置和 key；有就直连 APNs，没有就经每台手机登记的推送中继（`docs/push-relay.md`），手机也没登记中继就不推 |

每台机器 `~/.persona/` 下还有 node 生成的 hooks（插件 `claude-hooks/`、`assistant-hooks/`，给更老的 Claude Code 的 `claude-settings.json`、`assistant-settings.json`）、`mcp.json`、`node.sock`、`uploads/`。

配对：hub 机器上 `~/.persona/bin/personad hub pair` 通过 HTTP 回环调用运行中的 hub，打印临时配对链接、二维码、到期时间，以及已开启 TLS 时的公钥指纹。`listen` 必须能接受 IPv4 回环连接。临时 token 有效期 10 分钟，只能兑换一次设备 token，兑换后最多宽限 2 分钟（均为协议策略）。`--legacy` 离线打印主 token 和旧格式二维码，不用于公网。二维码新参数见 [协议](protocol.md#临时配对)。

## 推送策略（`hub/push.go`、`hub/notify.go`）

- 需要你（提问 / 计划 / 授权 / 终端）：立即推，`time-sensitive`，带 `category: INTERACTION_<KIND>`（App 注册了对应按钮）。
  hook 拦下的对话框电脑上的 TUI 不显示，人在电脑前也答不了，所以不等。
- 做完了：照 Ovis（`github.com/devswha/ovis` 的 `server/push.ts`，MIT）的思路，推送是给不在电脑前的人的：
  - working → done / idle 后等 `notify.done_delay_seconds`（60 秒）再决定；期间恢复工作就当同一轮（开始时间不变），
    变成 idle（在 herdr 里看过）或在 App 里看过（`seen_at`）就不推；
  - 还有后台命令 / 子 Agent 在跑：先不推，等它们回报、Claude 处理完回报的那一轮结束后推一次（`WokeByTask` 的轮次单独出现时不推）；
  - 一轮的时长 = 第一次进入 working 到最后一次结束；每台设备按自己的设置（`prefs.done`：`off` / `long` / `always`）决定收不收，
    `long` 只收不短于 `long_turn_seconds` 的；`prefs.input` 关掉就不收「需要你」。
  - 屏蔽的 Agent、助手这一轮已经用 `notify_user` 推过的，都不推。hub 重启后进行中的轮次从重启后第一次看到它算起。

## 关停（`hub/shutdown.go`）

本次确认了退出路径的缺陷，尚不能确认 NAS 当时的数据库等待原因。旧代码虽然通过 BaseContext 将取消传给了 WebSocket，但 HTTP Shutdown 不等待已经 hijack 的连接；设备会话收尾与 WebSocket 处理协程之间也没有等待关系。最后的 pgxpool.Close 会等待所有借出的连接归还，原先没有上限。空闲 WebSocket 不会一直借用数据库连接，不能仅凭在线手机和超时日志认定存在行锁或连接泄漏。

现在先关闭明文和 TLS 监听，再向已登记的 WebSocket 发送 1001 going away。关闭握手最多留 500 毫秒，随后取消连接 context、强制关闭未响应的连接，并等待处理协程和写循环退出。正在登记设备会话的连接也纳入跟踪，停止接收后不再增加等待对象。然后收尾 HTTP、设备会话和锁屏测试，等待 node 连接退出，最后关闭连接池。hub 关停导致的设备断开记为 hub_stopped，不计入手机断线次数。

配置项 shutdown_timeout_seconds 指定整个关停的总预算，缺省或非正值使用 8 秒。单阶段预算为 2 秒，受总预算约束；握手预算为 500 毫秒。这些数字是退出策略：总预算允许最多消耗四份完整的阶段预算，避免各阶段最坏等待时间不断累加。它们不是性能估计，NAS 和真实手机网络尚未实测。部署方应让外部终止器的宽限覆盖配置的总预算。

阶段超时会记录阶段名和错误，然后继续后续收尾。即使某个清理操作不响应取消，总预算到期后 Run 也会记录错误并返回，现有命令入口据此退出进程。连接池没有强制回收借出连接的 API；CloseContext 超时后，后台关闭仍可等待借用者，但不会再阻止命令退出。

SSH 一侧没有数据库连接池。hub 已通过 CommandContext 在取消时终止本地 node 或 SSH 子进程；本次增加退出等待和管道等待上限，没有修改 node 协议或远端模块。应用消息和能力协商保持原格式，现有 iOS 无需修改。

测试使用专用测试库、临时配置目录和本地假飞书服务，调用真实 Run。客户端收到 hello 后保持连接，再取消 hub context。断言覆盖关停返回、设备停止记录、1001 关闭码，以及返回后没有 WebSocket 处理或写循环协程残留；不响应关闭握手的客户端另测。连接池测试持有一条借出连接，验证 CloseContext 超时返回，归还后关闭完成。总预算测试模拟不响应取消的清理操作。

2026-10-31 本地测量，从取消 context 到 Run 返回，单位为毫秒。每种场景各 5 个样本，少于 10 个，不能作为性能基线；分位数采用最近秩法。

| 客户端 | 样本数 | min | p50 | p90 | max | mean |
| --- | --- | --- | --- | --- | --- | --- |
| 正常读取并回应关闭 | 5 | 3.063 | 3.868 | 7.107 | 7.107 | 4.441 |
| 不读取、不回应关闭 | 5 | 506.592 | 508.780 | 510.670 | 510.670 | 508.478 |

验证已通过：服务端编译、hub 与 db 的相关测试，以及 hub 全包测试。沙箱内 Go 无法取得工作树的 VCS 状态，执行时设置了 GOFLAGS=-buildvcs=false，仅关闭构建版本信息嵌入。首轮测试意外触发既有飞书逻辑的容器只读探测，随后已改为本地假服务隔离，容器未作修改。

## 推送给 App 的量（2026-11-02，耗电排查）

App 开着时连着 hub 的 WebSocket，hub 把各种变化广播给它；真实 hub 上录的 19 分钟（`docs/testing.md`「推送回放」）：
每分钟 105 条、2.6 MB，其中 `sessions`（一台机器的整份会话列表）37 条、2.6 MB，是几乎全部的字节。手机在蜂窝网络下一直收，射频回不到空闲。

- 新 App 收增量（`sessions_delta`，`hub/sessions_push.go`，`docs/protocol.md`「会话增量」）：连上时一份不带 Codex 历史的快照，之后只有变了的会话。
  回放：会话推送每分钟 2605 KB → 79 KB（-97%），全部消息 2616 KB → 90 KB；连上时 602 KB → 227 KB。
- 一模一样的整表不再发（`broadcastSessions`，老 App）：提问出现 / 答完、看过一个会话、Codex 卡片都会把那台机器的列表再发一遍，常常没变。
  每台机器记着最后发出的那条（整条消息的字节），一样就跳过；记录和入队在同一把锁里，记下的就是 App 最后收到的。回放：37 → 30 条 / 分钟，
  字节只少 6%：剩下的都是真变了（一个会话的 `rev`、`updated_at`、最后一句），要靠增量协议（只发变了的会话）。
- 跟不上的连接直接断开：某个 App 的发送队列满了（落后 512 条）以前是丢掉这一条，现在像 `sendJSON` 一样断开，App 重连后整份重来。
  不发重复的列表以后，丢掉的那条不会再被下一条一样的补上。
- 实时活动的推送节奏见 `docs/live-activity.md`「推送节奏」。

## 自己更新（`hub/selfupdate.go`，`docs/hub-update.md`）

发布版（`channel=release`）的 hub 每 6 小时看一次发布站点的 `latest.json`，有更新的版本、助手和 Agent 都空闲时自己换二进制、重启，起不来就回到上一版。
- 文件都在 `~/.persona/update/`：`<build>/` 是下载（node 二进制在 `nodes/`，确认后放进 `~/.persona/bin/nodes/`）；`pending.json` 是进行中的更新（确认完就删）；
  `rolledback.json` 是刚回滚的那次（旧版本起来后记进更新记录、标成坏版本再删）；`watch.log` 是看门狗的输出，没有服务管理器时回滚后起的 hub 输出在 `hub.log`。
- 上一版在 `~/.persona/bin/jarvisd.prev`。手动退回：`cp ~/.persona/bin/jarvisd.prev ~/.persona/bin/personad && systemctl --user restart jarvisd`（launchd 下 `launchctl kickstart -k`），
  再在 App 里关掉自动更新，免得又装回去（回滚过的版本本来就不会自动再装）。
- 关掉自动更新：App 设置里的开关，或 `PUT /api/v1/hub/update/settings {"auto": false}`。
- 通知：自动更新确认后推「hub 已更新到 X」，回滚的那次记进更新记录时推「hub 更新没成功」和原因（`updater.notify`，所有设备，thread `hub_update`）；手动点的更新不另推。
- 直接部署（`deploy.sh` / 主会话的脚本）：build 号是编译时间，比当时的公开版新，不会被降级；正在更新的那几分钟里直接部署也不会被看门狗撤销。

## 运维

- 机器离线：App 里显示 ssh 的最后一行错误。NAT 后面的电脑要先建好反向隧道（`docs/config.md`）。
- 机器显示在线但所有任务立即报 `rpc: connection closed`：检查启动器和助手接口是否同时返回 502。
  关闭 RPC 不会自动关闭 SSH 的 stdout；连接循环必须同时等待 `peer.Done()`，随后取消旧 SSH 子进程并重连，
  否则会一直保留已关闭的 peer 和过期的在线状态。`TestMachineReconnectReplacesClosedPeer` 用真实子进程覆盖此场景。
- 手动调接口：内网可用 `curl -H "Authorization: Bearer $TOKEN" http://<hub 地址>:18660/api/v1/machines`；公网使用设备 token、HTTPS 和公钥钉扎，完整命令见 [网络文档](network.md#命令行验证)。
- 公网鉴权失败含 WS 握手失败，单独记 `auth source=… path=… status=… code=… failures=… suppressed=…`；来源按 IPv4 /24、IPv6 /64 打码，日志路径只保留固定 API 路径，不记录 token。限流返回 429 和 `Retry-After`；日志节流由 `security.auth_log_interval_seconds` 控制。
- 网络问题先在 hub 机器上跑 `personad hub doctor`（只读不改），见 [网络排查](network.md#网络排查)。
- 管理界面登录失败记一行 `admin: wrong password from <打码的来源>`；设密码、改密码、改访问范围、加删电脑、撤销设备、生成配对码各记一行 `admin: …`，
  不记密码和配对码。管理界面的 401 / 404 也按上面的规则记请求 ID。
- 请求 ID：每个响应带 `X-Request-Id`（App 发了 `X-Client-Request-Id` 就沿用它）。hub 只记失败（≥400）和慢（>2 秒）的请求，
  一行 `[<id>] 方法 路径 -> 状态码 (耗时)`，不记 query、body、token。App 的 5xx 错误提示后面带「（请求 <id>）」，拿它在
  `journalctl --user -u jarvisd` 里搜那一行。
- herdr 连不上（socket 没了、herdr 重启）：node 每 30 秒最多记一行 `herdr: … failed`（带这段时间失败了几次），恢复时记一行
  `reachable again`，经 ssh 的 stderr 进 hub 日志。
