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

## 配置与数据（example-machine `~/.jarvis/`，都是 0600，不入库）

| 文件 | 内容 |
| --- | --- |
| `hub.json` | `listen`、`token`、`public_url`、`hook_timeout_minutes`、`machines`（id / name / kind local\|ssh / ssh_host / icon） |
| `secret.key`、`mail_accounts.json` | 邮箱凭据（AES-256-GCM） |
| `phone_tools.json` | iPhone 最近一次声明的工具（手机离线时 Claude 也能看到工具列表） |
| `devices.json` | APNs 设备 token |
| `apns.json` | `{key_id, team_id, key_path, topic}`；没有它推送就关闭 |

每台机器（含 Mac）`~/.jarvis/` 下还有 node 生成的 `claude-settings.json`（hooks）、`mcp.json`、`sessions.json`（jarvis 启动过的会话和启动器）、
`node.sock`、`uploads/`。

配对：example-machine 上 `~/.jarvis/bin/jarvisd hub pair` 打印地址、token 和二维码（`jarvis://pair?url=…&token=…`）。

## 运维

- 机器离线：App 里显示 ssh 的最后一行错误。`mac-home` 的隧道（2224）还没装，见 `docs/config.md`。
- 手动调接口：`curl -H "Authorization: Bearer $TOKEN" http://hub.example.com:18660/api/v1/machines`。
