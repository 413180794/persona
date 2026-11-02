# jarvis-runtime-sdk

把一个没有终端、自己管会话的 Agent 运行时（runtime）接进 Jarvis 的 SDK：`runtime.sock` 协议的 runtime 一端。
规范（要实现什么、内容和交互的约定、验收清单）见 [`docs/runtimes.md`](../../docs/runtimes.md)，线上协议见 `docs/protocol.md`「Runtime ↔ node」。

| 文件 | 内容 |
| --- | --- |
| `index.js` | 入口：`createRuntime`（driver + 选项）和 item 的工具（`light`、`clipInput`、`snippet`、`summaryOf`、`interactionId`） |
| `lib/runtime.js` | 连接 / 重连 / `runtime.hello`、列表推送、watch 补发和连续性、逐字节流、问手机、「谁先回答」 |
| `lib/items.js` | 页里的长度上限、截断说明、稳定的交互 id |
| `lib/wire.js` | 和 `server/internal/rpc` 同一种 JSON 行 RPC |
| `testing/fake-node.js` | 单元测试用的假 node（临时 socket，记录 runtime 说了什么，像 node 一样调它） |
| `testing/node-harness.mjs` | 端到端用：起一个真的 `jarvisd node --stdio`（隔离的 HOME），脚本当 hub |
| `examples/echo-runtime.mjs` | 最小的完整 runtime，新 runtime 的骨架 |
| `e2e/echo.mjs` | 验收模板：`JARVISD=<jarvisd> node e2e/echo.mjs` |

`npm test` 跑单元测试。没有依赖、不用构建：Node 22.19+。
