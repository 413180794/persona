# 扩展 SDK

写 Persona 扩展用的官方 SDK（#1843）。现在只有从 `schema/extension/` 生成的协议和 View 类型；进程协议的运行时、模板和命令行在阶段 4（#1853）加进来。

| 目录 | 内容 |
| --- | --- |
| `go/` | Go module `github.com/nwverse/persona/sdk/go`，不依赖 hub 的代码，也不依赖第三方库。`wire/` 是生成的类型 |
| `ts/` | TypeScript。`src/wire.ts` 是生成的类型 |

生成的文件别手改，改 `schema/extension/` 再跑 `python3 scripts/gen/extension_schema.py`，见 `schema/extension/README.md`。
