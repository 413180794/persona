# 屏幕夹具

`pane read --source visible` 的真实屏幕，`<agent>-<版本>-<场景>-<宽度>.{ansi,txt}`：`.ansi` 带颜色（`format: ansi, strip_ansi: false`），
`.txt` 是同一屏的纯文本。Claude Code 改界面时加一份新版本的，老版本的留着。

- `claude-2.1.312-*`：本仓库在一个临时目录里起的 claude 上录的（路径换成了 `/private/tmp/project`）。
- `claude-rewind-2.1.304-*`：取自 Ovis（github.com/devswha/ovis，`server/fixtures/`，MIT License，Copyright (c) 2026 devswha）。

录制：herdr 里开一个 tab 跑 claude，用 socket 的 `pane.read` 各读一次 ansi 和纯文本。入库前确认没有路径、主机名、账号。
