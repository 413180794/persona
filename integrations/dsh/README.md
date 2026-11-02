# jarvis-dsh-runtime

DSH（DeepSeek Harness）的 Web 插件：把这台电脑上 `dsh web` 的会话接进 Jarvis（连 jarvisd node 的 `~/.jarvis/runtime.sock`）。
它是按 `docs/runtimes.md` 规范接入的第一个 runtime，建在 [`integrations/runtime-sdk`](../runtime-sdk/README.md) 上。
说明、安装、配置和限制见 [`docs/dsh.md`](../../docs/dsh.md)。

| 文件 | 内容 |
| --- | --- |
| `index.js` | Cordis 插件入口，只是加载器（SDK 的 `createLoader`）：DSH 留着它，插件代码从 `~/.jarvis/runtime-packages/` 里带哈希的版本目录加载，node 带来新版本时自己换上（`docs/dsh.md`「自动更新」）。尽量不改它：改了要在 DSH 里手动重新加载一次 |
| `lib/plugin.js` | 实现的入口（加载器的 `api` / `start`）：取 `sessionController` 等服务，校验配置，启动 runtime |
| `lib/dsh.js` | DSH 的 driver：列会话、翻页、发送 / 中断 / 新建 / 改名，事件和两条应答链接到 SDK |
| `lib/map.js` | DSH 会话事件 → Jarvis item 的纯映射（基于 DSH 的平台可以复用） |
| `test/` | `npm test`（`node --test`，不需要 DSH） |
| `e2e/run.mjs` | 端到端：真的 `dsh web` + 真的 `jarvisd node`，见 `docs/dsh.md`「验证」 |
| `e2e/update.mjs` | 端到端：node 带另一个版本的插件时，插件自己换上、照常工作，DSH 重启后跑换上的那个 |

没有依赖、不用构建：Node 22.19+ 直接跑。SDK 按相对路径引用，从仓库的检出目录装。
