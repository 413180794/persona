# 经验：协作与流程：多个 Agent 同时改仓库、提交、迁移号、脚本

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 多个 agent 同时在这个仓库干活：模拟器各用各的（`SIM=<udid> scripts/app/sim.sh`），别人没写完的文件编译不过时在 `git worktree` 里构建自己的改动；
  提交只暂存自己的文件 / hunk。
- mock hub 的端口也会撞（2026-10-31：别的车道的 `mock_hub.py` 占着 18794，自己带 `MOCK_DRAWER=1` 的 mock 起不来，测试连到别人的 mock 上，
  报「对话没加载」）：起 mock 前 `lsof -nP -iTCP:<端口> -sTCP:LISTEN` 看一眼，失败要看 mock 的日志，别只看测试报错。
  停自己的 mock 按端口停（`kill $(lsof -t -iTCP:<端口> -sTCP:LISTEN)`），别 `pkill -f mock_hub.py`：会把别的车道正在用的 mock 一起杀掉（2026-10-31 踩过）。
  经 ssh 远程 `pkill -f <路径>` 也会匹配到 ssh 起的那个 `bash -c '…'` 自己（命令行里有同一个路径），把这次 ssh 连同后面的命令一起杀掉：按记下的 pid / 进程组停。
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
- 服务端测试以前都连同一个 `jarvis_test` 库：几个车道和部署同时跑，互相清掉、迁移对方的表，测试拿到空的或别人的数据随机失败（#545、#556、#583）。
  现在有库的测试包在 `TestMain` 里调 `dbtest.Main`（`server/internal/db/dbtest`）：每个测试二进制在 `JARVIS_TEST_DATABASE_URL` 旁边建自己的
  `jarvis_test_<时间>_<随机>`，跑完删掉（被杀掉留下的，6 小时后由下一次运行清掉）；测试里再起的子进程（假 node）沿用父进程的库。
  新加用到数据库的测试包也要这样加 `TestMain`，别直接连共用的库。角色没有建库权限时退回成加锁、排队用共用的库。
- 新加了大依赖（#167 的 tailscale / gvisor，单个 zip 21 MB）后第一次在 hub 机器上 `go test`：那台机器出网慢，`go` 在下载上挂了二十多分钟，
  最后报 `unexpected EOF`（2026-11-01）。办法：按 `server/go.sum` 列出 hub 的 `~/go/pkg/mod/cache/download` 里缺的文件，在开发机上从
  proxy.golang.org 下好（.zip / .mod / .info），rsync 到 hub 的一个目录，测试时 `GOPROXY=file://<那个目录>,https://proxy.golang.org`。

- `gh pr edit` 更新正文时可能附带查询组织 / 讨论字段，只带 `repo` scope 的 token 会报缺 `read:org`（2026-11-02）。
  更新正文不需要扩权限：把 `{"body": "…"}` 作为 JSON 标准输入交给 `gh api --method PATCH repos/<owner>/<repo>/pulls/<number> --input -`，只访问仓库的 PR 接口。

- 在独立 worktree 跑隐私扫描时，先进入那棵工作树再执行脚本：`privacy_check.py` 用当前目录的 `git rev-parse --show-toplevel` 取扫描根，不是脚本所在目录（2026-11-02，#341）。从主工作区执行 `/tmp/<worktree>/scripts/privacy_check.py` 实际扫描的是主工作区，也会读它的 `CLAUDE.local.md`；两边结果不同先确认扫描根，不要跳过或放宽规则。
- `gh pr checks` 的旧版本不支持 `--fail-fast`（2026-11-02，#341）：先看 `--help`，用支持的 `--watch` / `--interval`；参数错误的退出码 1 不等于 CI 失败，要看具体 check 的结论。

## 临时的测试 hub

- 要在真实机器上验证、又不能动生产 hub：在 hub 机器上用另一个 HOME（`HOME=/tmp/x jarvisd hub`，hub.json 里换 listen 端口、
  单独建一个库）起一份，`kind: local` 的机器就是同一份二进制起的 node，HOME 不同所以碰不到生产 node 的 socket 和配置。用完删库、删目录。
- `ssh -L` 前先看本地端口有没有被占：别的车道可能已经在同一个端口上开着到生产 hub 的隧道，`ExitOnForwardFailure` 报错之前的请求会打到生产 hub。
  连上后先看 `/api/v1/health` 的 `version` 是不是自己那份。
- 远程 `pkill -f <模式>` 会匹配到执行它的那条 ssh 命令本身（命令行里也有这个模式），把自己的会话杀掉、后面的清理不跑：用 `pgrep` 拿到 pid 再 kill，或者用不会出现在命令行里的写法。
- 远程用 `nohup … &` 起的后台进程会让 ssh 一直不返回（继承了会话），工具调用就卡到超时：重定向输出之外再加 `setsid`，或者起完单独一条 ssh 去查。
- 提交前用 `git var GIT_AUTHOR_IDENT` 确认作者是 GitHub 身份（比如 noreply 邮箱，仓库级 `git config --local` 设）：全局 git 身份如果是公司的真名和邮箱，
  它会留在 PR 的提交记录里（`refs/pull/<n>/head`，PR 合并后改不掉、删不了，只有 GitHub 支持能清），
  而且 squash 合并时 GitHub 会把和合并人不同的作者写成 `Co-authored-by` 带进 master 的提交信息（2026-11-02）。worktree 共用仓库级配置。
