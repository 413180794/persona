# 屏幕夹具

`pane read --source visible` 的真实屏幕，`<agent>-<版本>-<场景>-<宽度>.{ansi,txt}`：`.ansi` 带颜色（`format: ansi, strip_ansi: false`），
`.txt` 是同一屏的纯文本。Claude Code 改界面时加一份新版本的，老版本的留着。

- `claude-2.1.312-*`：本仓库在一个临时目录里起的 claude 上录的（路径换成了 `/private/tmp/project`）；`-suggestion-171` 是一轮结束后输入框里的灰色建议（`-suggestion-typed-171` 是在它上面打了字）。
- `claude-rewind-2.1.304-*`：取自 Ovis（github.com/devswha/ovis，`server/fixtures/`，MIT License，Copyright (c) 2026 devswha）。
- `codex-0.153.4-*` / `codex-0.163.0-*`：本仓库在一个独立的 herdr 会话里起的 Codex（ChatGPT 登录）上录的，内容是编的（横幅颜色、example.com），
  路径换成了 `/private/tmp/project`；`@` 文件选择那屏会列出这台机器装的插件和 skill，没收进来。宽度是无头 herdr 窗格的 120 列。

录制：herdr 里开一个 tab 跑 claude，用 socket 的 `pane.read` 各读一次 ansi 和纯文本。入库前确认没有路径、主机名、账号。
