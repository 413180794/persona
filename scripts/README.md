# 脚本目录

脚本按用途分组，优先使用下面的正式路径。脚本从任意工作目录调用时，都能找到所属仓库；迁移版本的构建 / 部署配置优先读取 `PERSONA_CONFIG` 或 `~/.config/persona/config.json`，兼容 `JARVIS_CONFIG` 和旧 `~/.config/jarvis/config.json`，不在仓库中保存个人值。脚本文件名和调用标识符以实际实现为准，不能按产品名盲替换；见 [配置与兼容说明](../docs/config.md#命名与兼容)。

| 目录 | 内容 | 常用入口 |
| --- | --- | --- |
| `app/` | iOS / macOS / Android 构建、运行、截图，DEBUG runtime CLI 和场景脚本，Mac 菜单栏 App「Persona Link」 | `app/sim-boot.sh`（建 / 开模拟器的唯一入口，simslim 瘦身）、`app/sim.sh`、`app/android.sh`、`app/mac_shot.sh`、`app/jarvisctl.py`、`app/mac-link.sh` |
| `deploy/` | hub / node 部署、数据库准备、安装器、skills 同步，Linux 上连接器的 systemd 包装 | `deploy/deploy.sh`、`deploy/install.sh`、`deploy/link-systemd.sh` |
| `release/` | App 和公开 personad 发版、TestFlight、App Store Connect、签名、bundle 隐私和权限文案检查 | `release/release.sh`、`release/publish.sh`、`release/asc_testflight.py`、`release/entitlements.py` |
| `mock/` | 假 hub、运行中切换场景、体验模式数据生成 | `mock/mock_hub.py`、`mock/mock_scenarios.py`、`mock/make_demo_data.py` |
| `assets/` | App 图标、可切换图标和形象包生成 | `assets/make_icon.py`、`assets/make_alt_icons.py`、`assets/make_persona.py` |
| `diag/` | 推送回放、窗格诊断、transcript 脱敏及统计 | `diag/agentsync_replay.py`、`diag/pane_probe.py`、`diag/redact_transcript.py` |
| `test/` | 服务端、真实运行时联调、GitHub 流程及目录结构回归 | `test/check-fast.sh`（提交前按改动跑相关快速检查）、`test/test-server.sh`、`test/test-herdr.sh`、`test/test-codex.sh` |
| `hooks/` | Git 提交 / 推送前的隐私检查（`pre-commit`、`commit-msg`、`pre-push`），合并 PR 的 `merge-pr.sh` | `git config core.hooksPath scripts/hooks`；合并：`scripts/hooks/merge-pr.sh <PR 号>` |
| `gen/` | 从 schema 生成代码：扩展协议和 View 的四端类型（`schema/extension/`）；扩展 SDK 的打包（`personad extension new` 用）和拷进扩展仓库 | `gen/extension_schema.py`（`--check` 只比对）、`gen/sdk_bundle.py`、`gen/sync-sdk.sh` |

## 顶层公共脚本

- `persona_identity.py`：运行根目录的公共解析器；不读取构建配置，也不使用环境文件中的凭据。`--service hub|node|link` 在没有显式新旧 HOME 环境时恢复对应角色的已安装用户服务根目录，尊重环境清空、用户 drop-in 和 `XDG_CONFIG_HOME`；省略角色时，多服务根冲突会停止而不猜测。外部 `EnvironmentFile` 只提取 `PERSONA_HOME` / `JARVIS_HOME`，支持按顺序覆盖、可选文件、通配符和列表清空；`UnsetEnvironment` 在文件之后生效。文件内容不会执行，其他字段不会返回或记录；必需文件缺失会在创建数据之前停止，显式 HOME 可直接选择根。`--prepare` 补默认兼容软链，`--prefix` 将同一解析逻辑带到远端执行。所有远端路径使用远端自己的 HOME。
- `jarvis_config.py`：所有脚本共用的配置读取与 Xcode 配置生成，位置不变。
- `privacy_check.py`：仓库和暂存内容的隐私检查，位置不变；Git hooks、网页构建和 CI 继续调用它。
- 常用 shell 命令保留顶层兼容入口：`sim.sh`、`android.sh`、`ui-test.sh`、`mac_shot.sh`、`deploy.sh`、`publish.sh`、`release.sh`、`test-server.sh`、`check-fast.sh`。
  它们只用 `exec` 转发到正式入口，原样传递参数、环境、标准输入和退出码，没有第二份实现。仓库内部调用和文档使用子目录路径。
- Python 脚本和其他工具使用正式路径，不保留平铺副本。仓库外的自动化请按本表迁移路径。

## 验证

从仓库根目录运行。脚本回归测试使用合成数据；服务端测试需要已配置的 hub 和真实 Postgres 测试库。运行实际部署、发布或真实 Agent 联调前，另按相关文档准备环境。

```sh
python3 -B -m unittest discover -s scripts/test -p 'test_*.py'
python3 -B -m unittest discover -s scripts/release -p 'test_*.py'
python3 -B -m unittest discover -s scripts/deploy -p 'test_*.py'
python3 -B -m unittest discover -s scripts/mock -p 'test_*.py'
python3 -B -m unittest discover -s scripts/diag -p 'test_*.py'
node scripts/test/pr_needs_issue_test.mjs
python3 scripts/privacy_check.py
./scripts/test/test-server.sh
```

目录回归测试检查兼容入口转发、配置 import、根目录推算、shell / Python 语法，以及 mock hub 的真实启动；`test/test_doc_paths.py` 检查现行文档（顶层文档，以及 `android/`、`integrations/`、`schema/`、`sdk/`、`spec/` 下 git 跟踪的 md）里反引号引用的仓库路径都还在（`docs/archive/` 不查），以及 `docs/` 顶层每篇文档都在 CLAUDE.md 的文档索引里。`test/test_doc_go_paths.py` 在同一批文档里查按包写的 Go 文件（`devices/deploy.go`，包是 `server/internal` 下的目录）都还在那个包里。Python 脚本要能在 macOS 自带的 Python 3.9 上跑：语法按 3.9 检查，注解里写 `X | None` 的文件要加 `from __future__ import annotations`（3.9 运行到 def 时就求值注解，`str | None` 会 TypeError，#579）；`datetime.fromisoformat` 在 3.9 上只认 3 或 6 位小数秒，解析 Go 写的时间先补齐到 6 位。新增脚本放到对应功能目录，回归测试也放在同目录；不要再往顶层增加功能脚本。
