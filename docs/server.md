# 服务端 jarvisd

Go，单一二进制，`server/cmd/jarvisd`。模式见 `docs/architecture.md`，协议见 `docs/protocol.md`。

## 代码

| 包 | 内容 |
| --- | --- |
| `internal/proto` | 线上类型 |
| `internal/rpc` | hub↔node 的双向 JSON-lines RPC |
| `internal/transcript` | Claude Code JSONL → `proto.Item`，增量读取、分页 |
| `internal/herdr` | herdr CLI 封装 |
| `internal/node` | node：会话列表、启动、发送、屏幕采样、hook / mcp 的 unix socket |
| `internal/hub` | hub：HTTP / WebSocket、机器连接、交互队列、手机工具转发、APNs |
| `internal/db` | Postgres：连接池、内嵌 SQL 迁移（`migrations/NNNN_*.sql`）、各表读写 |
| `internal/mail` | IMAP/SMTP、加密账号存储、邮件 HTTP 接口和 MCP 工具（`docs/mail.md`） |
| `internal/mcpserver` | `jarvisd mcp`：给 claude 的 stdio MCP server |

## 部署

```sh
./scripts/deploy.sh                    # 编译 linux/amd64 + darwin/arm64，装到所有机器，重启 example-machine 上的 hub
./scripts/deploy.sh example-machine example-machine  # 只装部分机器
```

- 二进制：每台机器 `~/.jarvis/bin/jarvisd`。node 由 hub 经 `ssh <别名> ~/.jarvis/bin/jarvisd node --stdio` 拉起，Mac 上不用常驻服务。
- hub：example-machine `systemd --user` 服务 `jarvisd`，监听 `:18660`。`journalctl --user -u jarvisd -f` 看日志。
- 换了 node 二进制后 hub 不会自动重连旧连接；`deploy.sh example-machine` 会重启 hub，或在 App「机器」页点「重新连接」。

## 数据库（Postgres）

example-machine 上的 docker 容器 `jarvis-postgres`（`postgres:18-alpine`，只监听 `127.0.0.1:18661`，数据卷 `jarvis-pgdata`，`--restart unless-stopped`）。
`scripts/db.sh` 幂等地创建 / 启动它，首次创建时生成密码写进 `hub.json` 的 `database_url`；`deploy.sh example-machine` 每次都会先跑它。
hub 启动时自动执行未跑过的迁移（`schema_migrations` 记录，advisory lock 防并发）。

| 表 | 内容 |
| --- | --- |
| `devices` | 推送设备（APNs token），APNs 返回 410 / BadDeviceToken 时自动删除 |
| `phone_tools` | iPhone 最近一次声明的 phone_* 工具（手机离线时 Claude 也能看到） |
| `node_sessions` | 每台机器上 Jarvis 启动过的会话：pane、启动器、目录（node 经 hub RPC `state.load` / `state.put` 读写，Mac 不直连数据库） |
| `mail_accounts` | 邮箱账号：非敏感字段 jsonb + AES-GCM 密文；密钥 `~/.jarvis/secret.key` 不进数据库 |
| `interactions` | 每次提问 / 计划 / 授权 / 终端交互及结局（resolved / dismissed / expired / cancelled）；hub 重启时把残留的 pending 标成 cancelled |
| `settings` | App 里改的全局设置（如助手的启动器） |

连库：`ssh example-machine 'docker exec -it jarvis-postgres psql -U jarvis -d jarvis'`。
测试：`./scripts/test-server.sh`（同步代码到 example-machine，在 `jarvis_test` 库上跑全部测试；本机没有 docker，DB 测试在本机会 skip）。

旧版的 `phone_tools.json`、`devices.json`、`mail_accounts.json`、`sessions.json` 首次启动时自动导入，改名为 `*.migrated`。

## 配置（example-machine `~/.jarvis/`，0600，不入库）

| 文件 | 内容 |
| --- | --- |
| `hub.json` | `listen`、`token`、`public_url`、`hook_timeout_minutes`、`database_url`、`machines`、`google.client_id`（Gmail 用 Google 账号登录的 iOS OAuth 客户端，`docs/mail.md`） |
| `secret.key` | 邮箱凭据的加密密钥（32 字节） |
| `apns.json`、`apns_key.p8` | 推送配置和 key；没有就关闭推送 |

每台机器 `~/.jarvis/` 下还有 node 生成的 `claude-settings.json`（hooks）、`mcp.json`、`node.sock`、`uploads/`。

配对：example-machine 上 `~/.jarvis/bin/jarvisd hub pair` 打印地址、token 和二维码（`jarvis://pair?url=…&token=…`）。

## 运维

- 机器离线：App 里显示 ssh 的最后一行错误。`mac-home` 的隧道（2224）还没装，见 `docs/config.md`。
- 手动调接口：`curl -H "Authorization: Bearer $TOKEN" http://hub.example.com:18660/api/v1/machines`。
