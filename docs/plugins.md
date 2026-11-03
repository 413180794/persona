# 插件

hub 可以带几个「插件」：hub 机器上的独立进程，给 App 加一块能力（目前只有语音对话一种）。插件可以是 personad 自带的
（`personad voice`，见下面「内置语音插件」），也可以放在各自的仓库（比如只能内部用的实现）；这里写 hub 怎么跑它们、App 和插件之间的契约。

## 配置

`~/.persona/hub.json`：

```json
{
  "plugins": [
    {
      "id": "voice",
      "name": "语音",
      "kind": "voice",
      "command": "/usr/bin/node",
      "args": ["~/.persona/plugins/voice/src/main.mjs"],
      "dir": "~/.persona/plugins/voice",
      "env": {"SOME_SETTING": "…"}
    }
  ]
}
```

- `id`：`a-z0-9_-`，最长 32；也是 App 访问它的路径（`/api/v1/plugins/<id>/…`）。
- `kind`：App 据此决定显示什么。现在只认 `voice`（下面的语音契约）。
- `command` / `args` / `dir`：`~/` 是 hub 机器的家目录（`command` 和 `dir` 会展开，`args` 原样传，插件自己展开）。
  personad 的 systemd unit 的 PATH 很短，`command` 写绝对路径。
- `disabled: true` 停用但保留配置。
- `config`：插件自己的设置（任意 JSON），原样放进环境变量 `JARVIS_PLUGIN_CONFIG` 交给插件。内置语音插件的上游就写在这里。
- 内置插件仍可用兼容配置值 `"command": "jarvisd"`（不带路径）指向 hub 自己这个二进制，换了安装位置、升级以后也不用改。产品 / 二进制的新名称不意味着插件配置里的特殊值已经迁移，具体接受哪些别名以运行时实现为准。
- 只写 `socket`、不写 `command`：插件跑在别处，hub 只连这个 socket（健康检查、转发），不启动、不重启它。
  用于 hub 机器访问不到插件依赖的服务时：插件跑在能访问的机器上，用 `ssh -R <hub 上的 socket>:<本机 socket> <hub 机器>` 把 socket 转过来；
  这时插件自己配 `JARVIS_HUB_URL`（hub 的 `public_url`）和 token。

## hub 做什么

- 启动插件进程（自己的进程组），退出就按 2 秒起、最长 60 秒的退避重启；hub 停的时候连子进程一起结束。输出逐行进 hub 的日志（`plugin <id>: …`）。
- 给它的环境变量：
  - `JARVIS_PLUGIN_SOCKET`：插件要在这个 unix socket 上起 HTTP 服务（`~/.persona/plugins/<id>.sock`，启动前 hub 会删掉旧的）。
  - `JARVIS_PLUGIN_ID`；有 `config` 时还有 `JARVIS_PLUGIN_CONFIG`。
  - `JARVIS_HUB_URL`、`JARVIS_HUB_TOKEN`：本机访问 hub API 的地址和 hub 的 token。插件是 hub 的主人自己配的，和 node 一样算 hub 的一部分，
    可以用这些读知识库、Agents 等。
- 每秒（起来以后每 10 秒）请求一次插件的 `GET /health`，200 就算 `running`。
- `GET /api/v1/plugins` → `{"plugins":[{id,name,kind,status,error}]}`，`status` 是 `starting | running | error | disabled`。
  `/health` 和 WebSocket hello 的 `features` 里有 `plugins` 时才有这个接口。
- `/api/v1/plugins/<id>/<path>` 原样转发到插件的 `/<path>`（任何方法，WebSocket 也是），去掉 App 的 `Authorization` 头和 `token` 参数：
  鉴权在 hub 做完，插件拿不到 App 的凭据。只给完整权限的设备（手表那种只读 + 回答的设备 403）。

## 语音（kind = voice）

全双工：手机一直送麦克风（包括静音），什么时候该说话由模型决定，App 不做 VAD、不做「按住说话」。

### `GET /models`

```json
{"models":[{"id":"…","name":"…"}],"default":"…"}
```

能选的模型（`name` 可省，没有就显示 id），`default` 是插件自己配的那个。

### `GET /voices?model=<id>`

```json
{"voices":[{"id":"…","name":"温柔桃子","gender":"female","languages":["zh"]}],"default":"…"}
```

这个模型能用的音色（不同模型的音色集可能不一样，不带 `model` 是默认模型的）。App 里选中的模型和音色存在本机，开始对话时带上。

### `GET /ws`（WebSocket）

文本帧是 JSON，二进制帧是音频（PCM16 little-endian，单声道）。

App → 插件：

| 帧 | 含义 |
| --- | --- |
| `{"type":"start","model":"<id>","voice":"<id>"}` | 第一帧；`model`、`voice` 都可省（用插件的默认） |
| 二进制 | 麦克风，采样率是 `ready` 里的 `input_rate`；`ready` 之前别发 |
| `{"type":"interrupt"}` | 打断正在说的回答 |
| `{"type":"text","text":"…"}` | 打字说一句（和说话等价） |

插件 → App：

| 帧 | 含义 |
| --- | --- |
| `{"type":"ready","input_rate":16000,"output_rate":24000,"model":"<id>","voice":"<id>"}` | 会话建好；采样率以这里为准，别写死 |
| 二进制 | 要播放的声音，采样率 `output_rate` |
| `{"type":"clear"}` | 丢掉还没播的声音（用户插话打断了，或者回答被取消） |
| `{"type":"transcript","id":"…","role":"user"\|"assistant","text":"…","final":false}` | 字幕：同一个 `id` 每次给**到目前为止的全文**，App 直接替换；`final` 后这条不再变 |
| `{"type":"tool","id":"…","label":"查知识库","done":false}` | 正在用工具（App 显示一行状态）；可以同时开着几个（比如交给主对话的事在后台跑），按 `id` 收，显示最新的那个 |
| `{"type":"error","message":"…","fatal":true}` | `fatal` 时插件随后关闭连接 |

关闭 WebSocket 就结束这次对话。

### App

- Android：点「Persona」tab 的标题，菜单里的「语音对话」（只在有 `running` 的 voice 插件时出现）进入语音页；页面右上角是「音色」
  （宿主接上模型配置页后是「模型配置 / 音色」菜单），模型读模型配置的用途 `plugin.<id>`，旧 hub 用插件自己的默认模型。
  录音用 `VOICE_COMMUNICATION` 音源 + 系统回声消除（外放时模型不会听到自己），见 `docs/android.md`。

## 内置语音插件（`personad voice`）

personad 自带一个 kind = `voice` 的插件，语音来自说 **GPT-Live 协议**的实时服务（代码在 `server/internal/voice`，提示词在
`server/internal/prompts/voice/`、工具说明在 `prompts/mcp/voice/`）。不用另装 node 和 SDK；可选：hub.json 里没有这个条目就没有它。

```json
{
  "plugins": [
    {
      "id": "voice",
      "name": "语音",
      "kind": "voice",
      "command": "jarvisd",
      "args": ["voice"],
      "config": {
        "providers": [
          {"id": "codex", "name": "GPT-Live", "dialect": "codex", "url": "wss://<网关>/v1/live", "key_file": "~/.persona/voice-gateway.key"},
          {"id": "rt", "name": "实时服务", "url": "wss://<实时服务>/v1/live", "key_command": "<打印凭据的命令>", "ssh": "<能访问它的机器>"}
        ],
        "owner": "<怎么称呼你>"
      }
    }
  ]
}
```

一个插件可以有几个上游（`providers`），App 里的模型列表是它们合在一起的（模型 id 是 `<provider>/<model>`，几个上游时名字后面带上游的名字），
选哪个模型就连哪个上游。`config.default` 指定默认模型（不写是第一个上游的默认）。

| 字段 | 说明 |
| --- | --- |
| `id`、`name` | `a-z0-9_-`；模型 id 的前缀和显示名 |
| `url` | 会话的 WebSocket：`wss://<host>/v1/live` |
| `dialect` | `live`（默认）：公开的 GPT-Live 协议；`codex`：Codex 跟 `gpt-live-1-codex` 说的那套（见下） |
| `key` / `key_file` / `key_command` | 凭据，作为 `Authorization: Bearer` 发出：直接写、文件第一行、命令（`sh -c`）打印的第一行。命令每次对话都跑一次，别处续期的凭据马上生效。凭据只放在 hub 机器上（hub.json 0600，或单独的文件） |
| `ssh` | hub 机器访问不到这个服务时，经那台机器连（`ssh -W host:port <机器>`，TLS 仍然是端到端的）；hub 机器要能免密 ssh 过去 |
| `models`、`default_model` | 列出来的模型；不写时 live 方言读服务的 `GET <url>/models`，codex 方言是 `gpt-live-1-codex` |
| `tools` | live 方言：在 `session.start` 里声明 Persona 的函数工具（查知识库、看 Agents、发给 Agent、交给主对话）。公开的 GPT-Live API 不收 `session.tools`，只有自己部署、支持它的服务才开 |
| `input_rate` | 麦克风采样率 16000 / 24000；默认 live 16000、codex 24000 |

### 两种方言

| | live（公开 GPT-Live） | codex（`gpt-live-1-codex`） |
| --- | --- | --- |
| 握手 | `Authorization: Bearer` | 加 `OpenAI-Alpha: quicksilver=v2`，模型在 URL 的 `?model=` |
| 第一帧 | `session.start{model, instructions, audio{format, output.voice}, delegation{type: client}, tools?}` | `session.update{instructions, audio.output.voice, delegation{type: client}}` |
| 麦克风 / 声音 | `session.input_audio.append` / `session.output_audio.delta{delta}` | `input_audio.append` / `output_audio.delta{audio}`（24 kHz） |
| 字幕 | `session.input_transcript.delta`、`session.output_transcript.delta`（只追加，没有「完」） | `input_transcript.added`、`output_transcript.added`，`turn.done` 给整句 |
| 交出去的事 | `session.delegation.created{target: client}`；有工具时 `response.event{function_call}` | `delegation.created{item{type: delegation, target: client, content}}` |
| 回答 | `session.thinking.append`（安静的进度）、`session.commentary.append{delegation_id}`（结果，模型会说出来），最多约 500 token | `delegation.context.append{channel: commentary / speakable}`，按 500 字节切 |
| 打字说一句 | `session.commentary.append{delegation_id: null}` | `session.context.append{channel: speakable}` |

模型把事交给客户端（delegation）时，插件把它交给 **Persona 主对话**（没有主对话就新开一个并设为主对话），先回一句安静的「交代下去了」，
主对话做完把最后的回复作为这个 delegation 的结果交回去，模型再口头转述；delegation 已经不在了（重连过）就作为一句新的上下文说出来。
App 那边这期间显示「Persona 主对话处理中」。开了 `tools` 的上游，模型调函数工具时插件直接执行（查知识库、看 Agents 走 hub 的 API），
`ask_jarvis` 也是交给主对话。

上游会话中途断了（服务重启、网络断）：App 的连接不断，显示「连接断了，重新连接中」，插件新开一个会话，把最近 40 句写进提示词，
一次对话最多重连 3 次。App 发 `interrupt` 时插件丢掉这次回答还没送出的声音（GPT-Live 没有打断的帧，模型自己听到插话会停）。

### 部署

`scripts/deploy.sh` 照常部署 personad 就带上了这个插件；hub.json 里写上上面的条目，`systemctl --user restart jarvisd`（改了 `plugins` 要重启 hub）。
本地试：`JARVIS_PLUGIN_SOCKET=/tmp/v.sock JARVIS_PLUGIN_CONFIG='{"providers":[…]}' personad voice`，然后对着这个 socket 发 `GET /models`、开 `/ws`。

### 验证（2026-11-03）

- 公开 GPT-Live 协议的实时服务（公司内部署）：打字说一句、模型出声和字幕、交给主对话、函数工具、经 `ssh` 跳板连，都实测过。
- Codex 方言：照 Codex 的源码（`codex-rs` 的 realtime_websocket）实现，单元测试用假的上游；兼容 OpenAI 的网关的 `/v1/live` 要有能接语音的 Codex 账号，
  没有时握手回 `503 no eligible upstream account`（插件把这句话原样显示在 App 里）。
