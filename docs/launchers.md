# 启动器与「设置 › 模型」

启动器决定 claude 用哪个网关、哪个模型。以前全是各台电脑 `~/.zshrc` 里调用 claude 的函数（`_claude_launchers` 列出来，
node 用 `zsh -ic <名字>` 跑）；2026-10-31 起「设置 › 模型」把它们和在 App 里加的放在一起管理。
代码：`server/internal/hub/launchers.go`、`server/internal/node/managed.go`；App `Views/Settings/ModelLaunchersView.swift`。

## 两种来源

| 来源 | 在哪定义 | 能做什么 |
| --- | --- | --- |
| **Jarvis 管理** | App 里新增：名字、Base URL、API Key、模型、小模型、额外参数 | 改、删 |
| **~/.zshrc** | 各台电脑 `~/.zshrc` 里的函数，打开这页时同步（node 缓存 1 分钟） | 看每台电脑上的函数定义（Key / Token 打码）、在 Jarvis 里隐藏 / 恢复 |

- 范围是**全局一份**：Jarvis 管理的每台电脑都能用；~/.zshrc 的标出来自哪几台（同名的合成一行）。离线电脑的列表用上次同步的（settings `launchers.zshrc`）。
- 删除 ~/.zshrc 的启动器**只在 Jarvis 里隐藏**（settings `launchers.hidden`），文件不动；新对话、助手的选择器里不再出现。
- 同名时 Jarvis 管理的优先（~/.zshrc 的那个被盖住）。名字只能是字母、数字、`_`、`-`，不能叫 `claude`。

## Jarvis 管理的怎么跑

- 存在 settings `launchers.managed`；API Key 用 `~/.jarvis/secret.key`（和邮箱凭据同一把）AES-GCM 加密，**接口从不返回 Key**。
- hub 在 node 连上时、每次改动后把完整设置（含 Key）推给所有 node（`launchers.managed`），node 只放内存。
- 启动时 node 敲进 herdr 窗格（或无头助手执行）的是 `jarvisd launch <名字> …`：这个子命令经本机 node.sock 取设置
  （`launcher.env`），去掉 shell 里已有的 `ANTHROPIC_*` 等变量，设上 `ANTHROPIC_BASE_URL`、`ANTHROPIC_API_KEY`、各档模型
  （`ANTHROPIC_MODEL`、`ANTHROPIC_DEFAULT_*_MODEL`、`CLAUDE_CODE_SUBAGENT_MODEL`，小模型给 haiku / small fast），再 exec claude。
  Key 不进文件、不进终端、不在命令行上。终端里也能直接敲 `jarvisd launch <名字>`。
- hub 重启或 node 重连之前没收到设置时，`jarvisd launch` 报「hub 还没发过来」。

## 接口

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/launchers` | 全部（先同步各台电脑的 ~/.zshrc） |
| `GET /api/v1/launchers/{name}` | 一个；~/.zshrc 的带 `definitions`（每台电脑的函数，打码） |
| `POST /api/v1/launchers` | 新增 `{name, base_url, model, small_model?, args?, api_key}` |
| `PUT /api/v1/launchers/{name}` | 改（`api_key` 不填就不变）；只对 Jarvis 管理的 |
| `DELETE /api/v1/launchers/{name}` | Jarvis 管理的删除；~/.zshrc 的隐藏 |
| `POST /api/v1/launchers/{name}/restore` | 取消隐藏 |
| `GET /api/v1/machines/{mid}/launchers` | 这台能启动的（选择器用）：它的 ~/.zshrc 去掉隐藏的，加上 Jarvis 管理的；每项带 `source` |

「设置 › 助手」的启动器选择器和新对话的一样读最后这个接口，所以用的就是这里的数据；助手页有「管理启动器」。
