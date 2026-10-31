# 插件

hub 可以带几个「插件」：hub 机器上的独立进程，给 App 加一块能力（目前只有语音对话一种）。插件的代码不在这个仓库里，
放在各自的仓库（比如只能内部用的实现），这里只有 hub 怎么跑它们、App 和插件之间的契约。

## 配置

`~/.jarvis/hub.json`：

```json
{
  "plugins": [
    {
      "id": "voice",
      "name": "语音",
      "kind": "voice",
      "command": "/usr/bin/node",
      "args": ["~/.jarvis/plugins/voice/src/main.mjs"],
      "dir": "~/.jarvis/plugins/voice",
      "env": {"SOME_SETTING": "…"}
    }
  ]
}
```

- `id`：`a-z0-9_-`，最长 32；也是 App 访问它的路径（`/api/v1/plugins/<id>/…`）。
- `kind`：App 据此决定显示什么。现在只认 `voice`（下面的语音契约）。
- `command` / `args` / `dir`：`~/` 是 hub 机器的家目录（`command` 和 `dir` 会展开，`args` 原样传，插件自己展开）。
  jarvisd 的 systemd unit 的 PATH 很短，`command` 写绝对路径。
- `disabled: true` 停用但保留配置。
- 只写 `socket`、不写 `command`：插件跑在别处，hub 只连这个 socket（健康检查、转发），不启动、不重启它。
  用于 hub 机器访问不到插件依赖的服务时：插件跑在能访问的机器上，用 `ssh -R <hub 上的 socket>:<本机 socket> <hub 机器>` 把 socket 转过来；
  这时插件自己配 `JARVIS_HUB_URL`（hub 的 `public_url`）和 token。

## hub 做什么

- 启动插件进程（自己的进程组），退出就按 2 秒起、最长 60 秒的退避重启；hub 停的时候连子进程一起结束。输出逐行进 hub 的日志（`plugin <id>: …`）。
- 给它的环境变量：
  - `JARVIS_PLUGIN_SOCKET`：插件要在这个 unix socket 上起 HTTP 服务（`~/.jarvis/plugins/<id>.sock`，启动前 hub 会删掉旧的）。
  - `JARVIS_PLUGIN_ID`。
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

- Android：「Jarvis」tab 顶栏的麦克风（只在有 `running` 的 voice 插件时出现）进入语音页；页面右上角的菜单选模型和音色。
  录音用 `VOICE_COMMUNICATION` 音源 + 系统回声消除（外放时模型不会听到自己），见 `docs/android.md`。
