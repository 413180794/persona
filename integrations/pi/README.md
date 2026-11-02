# jarvis-pi-runtime

pi（`@earendil-works/pi-coding-agent`）和 pi-web（`@agegr/pi-web`）接入 Jarvis：pi 扩展加 pi-web 伴随进程，建在
[`integrations/runtime-sdk`](../runtime-sdk/README.md) 上。说明、安装、配置和限制见 [`docs/pi.md`](../../docs/pi.md)。

| 文件 | 内容 |
| --- | --- |
| `index.js` | pi 扩展入口：每个 pi 进程、每个版本一个宿主，每个会话把自己登记进去；新版本的宿主接管旧的（`docs/pi.md`「自动更新」） |
| `companion.mjs` | pi-web 伴随进程：列磁盘上的 pi 会话，经 pi-web 的接口新建 / 唤醒；从存放目录跑时装好 node 的新版本就退出，等保活拉起 |
| `install.mjs` | 一次性切换到自动更新：把这份包装进 `~/.jarvis/runtime-packages/jarvis-pi-runtime/current/`，打印 `pi install` 和伴随进程的路径 |
| `lib/pi.js` | 宿主（runtime driver）：列表、翻页、发送 / 中断 / 改名 / 新建、逐字、授权、`ask_user` |
| `lib/map.js` | pi 会话条目 → Jarvis item 的纯映射（会话树、分支） |
| `test/` | `npm test`（`node --test`，不需要 pi） |
| `e2e/run.mjs`、`e2e/pi-web.mjs` | 端到端：真的 pi / pi-web + 真的 `jarvisd node` |
| `e2e/update.mjs` | 端到端：node 带新版本时，pi-web 里的扩展原地换上、伴随进程装好后被拉起，照常工作 |

没有依赖、不用构建：pi 自己提供 `@earendil-works/pi-coding-agent` 和 `typebox`。
