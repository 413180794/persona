# 助手的 skills（副本）

| 目录 | 来源 | 版本 |
| --- | --- | --- |
| `lark-connector/` | https://github.com/Eric-2369/lark-connector 的 `.agents/skills/lark-connector`（`SKILL.md`、`references/`、`scripts/ol.sh`；不要 `evals/` 和要 Node 的 `scripts/*.mjs`） | `e6f4715c2543bf47f9974c4de1fa603dd08208cb`（2026-10-30） |

原样拷贝，不改内容（改了下次同步会被覆盖）。**不编进 jarvisd**（不能再分发）：放进跑助手那台机器的 `~/.jarvis/skills/`，
node 在助手进程启动时把它链接到助手目录的 `.claude/skills/` 下（`docs/hub-update.md`「skill 从二进制里拿出来」）；Jarvis 怎么用它见 `docs/assistant.md`「飞书：lark-connector skill」。

同步上游：`scripts/deploy/sync_lark-connector_skill.sh <lark-connector 的 checkout>`，看 diff（尤其 SKILL.md 的「Confirm before acting」和
references 里的写接口）后提交；写接口变了要对照 `node/larkgate.go` 的默认规则。
