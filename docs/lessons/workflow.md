# 经验：协作与流程：多个 Agent 同时改仓库、提交、迁移号、脚本

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 多个 agent 同时在这个仓库干活：模拟器各用各的（`SIM=<udid> scripts/sim.sh`），别人没写完的文件编译不过时在 `git worktree` 里构建自己的改动；
  提交只暂存自己的文件 / hunk。
- mock hub 的端口也会撞（2026-10-31：别的车道的 `mock_hub.py` 占着 18794，自己带 `MOCK_DRAWER=1` 的 mock 起不来，测试连到别人的 mock 上，
  报「对话没加载」）：起 mock 前 `lsof -nP -iTCP:<端口> -sTCP:LISTEN` 看一眼，失败要看 mock 的日志，别只看测试报错。
  停自己的 mock 按端口停（`kill $(lsof -t -iTCP:<端口> -sTCP:LISTEN)`），别 `pkill -f mock_hub.py`：会把别的车道正在用的 mock 一起杀掉（2026-10-31 踩过）。
- 多个 agent 同时往 master 推：rebase 冲突时不要用自己的旧副本整文件覆盖别人的改动（2026-10-25 一次 rebase 把浅蓝气泡、通知跳转修复整个盖回去了）；
  解决冲突只改冲突块，push 前 `git diff origin/master...HEAD --stat` 看看有没有动到不是自己的文件。打 TestFlight 前确认 master 是修好的那一版。
- 多个 agent 并行改同一仓库（飞书工作台 6 个 worker，见 `docs/feishu.md`）：每人独立 worktree + 独立克隆模拟器；新增类型带领域前缀
  （`FeishuSearchXxx`…，否则 master 撞名编译失败）；rebase 冲突时自己目录外的文件一律取 origin/master 版本，push 前看
  `git diff origin/master --stat` 只含自己的文件（14fe469 曾用旧副本覆盖半个仓库，941d067 恢复）；主工作区有别人未提交的改动时，
  自己的修复在 `git worktree add --detach /tmp/x origin/master` 里做。
- 数据库迁移号按车道分段（00x1…），挑号前先看所有 worktree 的 `server/internal/db/migrations/` 和 `jarvis_test` 库的 `schema_migrations`：
  别的车道没合的迁移可能已经占了号（2026-10-30 0051 撞号，改用 0061）。
- 2026-11-01 `ios/` 改名为 `apple/`（iOS、手表、macOS 三个 App 和共享代码在同一个 XcodeGen 工程里）：老的分支 / worktree rebase 时，
  新建在 `ios/` 下的文件要手动挪到 `apple/`（改动已有文件 git 会跟着改名走）。
- 2026-10-28 改写过一次全部 git 历史（去掉内网信息和个人信息，GitHub 仓库删了重建）：此前的提交号全部失效，
  别的机器上的旧克隆要删掉重新 clone，不能 push，否则旧历史会被推回去。
- 等后台任务 / 日志的脚本一律带超时，成功、失败、超时都退出并打印一行结论（2026-10-29 一个只等成功的 `until grep` 空等了五个多小时）。
  脚本里顺手起的常驻进程（mock hub）要把整个后台组的输出重定向走：`( cd … && exec python3 … ) > log 2>&1 < /dev/null &`。
  写成 `(cd … && python3 … > log 2>&1 &)` 时，后台那层 shell 还拿着脚本的 stdout，`脚本 | tail` 永远等不到结尾，
  脚本早跑完了也不会唤醒等它的会话（2026-11-01 停了将近 6 小时）。
- macOS 的 sed 不认 `\b`，整词替换用 `perl -pi -e 's/\bFoo\b/Bar/g'`。
- `scripts/test-server.sh` 在两个 checkout 里同时跑会共用同一个 `jarvis_test` 库，DB 测试会互相干扰随机失败：并发跑时单独重跑失败的包确认。
- 新加了大依赖（#167 的 tailscale / gvisor，单个 zip 21 MB）后第一次在 hub 机器上 `go test`：那台机器出网慢，`go` 在下载上挂了二十多分钟，
  最后报 `unexpected EOF`（2026-11-01）。办法：按 `server/go.sum` 列出 hub 的 `~/go/pkg/mod/cache/download` 里缺的文件，在开发机上从
  proxy.golang.org 下好（.zip / .mod / .info），rsync 到 hub 的一个目录，测试时 `GOPROXY=file://<那个目录>,https://proxy.golang.org`。

## 临时的测试 hub

- 要在真实机器上验证、又不能动生产 hub：在 hub 机器上用另一个 HOME（`HOME=/tmp/x jarvisd hub`，hub.json 里换 listen 端口、
  单独建一个库）起一份，`kind: local` 的机器就是同一份二进制起的 node，HOME 不同所以碰不到生产 node 的 socket 和配置。用完删库、删目录。
- `ssh -L` 前先看本地端口有没有被占：别的车道可能已经在同一个端口上开着到生产 hub 的隧道，`ExitOnForwardFailure` 报错之前的请求会打到生产 hub。
  连上后先看 `/api/v1/health` 的 `version` 是不是自己那份。
- 远程 `pkill -f <模式>` 会匹配到执行它的那条 ssh 命令本身（命令行里也有这个模式），把自己的会话杀掉、后面的清理不跑：用 `pgrep` 拿到 pid 再 kill，或者用不会出现在命令行里的写法。
- 远程用 `nohup … &` 起的后台进程会让 ssh 一直不返回（继承了会话），工具调用就卡到超时：重定向输出之外再加 `setsid`，或者起完单独一条 ssh 去查。
