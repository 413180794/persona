# jarvis-pi-runtime

pi（`@earendil-works/pi-coding-agent`）和 pi-web（`@agegr/pi-web`）接入 Jarvis：pi 扩展加 pi-web 伴随进程，建在
[`integrations/runtime-sdk`](../runtime-sdk/README.md) 上。说明、安装、配置和限制见 [`docs/pi.md`](../../docs/pi.md)。

| 文件 | 内容 |
| --- | --- |
| `index.js` | pi 扩展入口：每个 pi 进程一个宿主，每个会话把自己登记进去 |
| `companion.mjs` | pi-web 伴随进程：列磁盘上的 pi 会话，经 pi-web 的接口新建 / 唤醒 |
| `lib/pi.js` | 宿主（runtime driver）：列表、翻页、发送 / 中断 / 改名 / 新建、逐字、授权、`ask_user` |
| `lib/map.js` | pi 会话条目 → Jarvis item 的纯映射（会话树、分支） |
| `test/` | `npm test`（`node --test`，不需要 pi） |
| `e2e/run.mjs`、`e2e/pi-web.mjs` | 端到端：真的 pi / pi-web + 真的 `jarvisd node` |

没有依赖、不用构建：pi 自己提供 `@earendil-works/pi-coding-agent` 和 `typebox`。
