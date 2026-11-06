# 多人 / 多 Agent 协作：先认领 issue 再动手

仓库是多人协作的，每个人和自己的 Agent 同时在改。出现过两个人修同一个 bug、修到一半才发现已经被别人修掉的情况，所以：
**动手前在 issue 里认领，PR 必须关联 issue**。整体流程（建分支、英文 PR、自行合并）见 `CLAUDE.md`「开发工作流」，这里是认领的细节、评论模板和可以照抄的命令。

## 规则

1. **开工前先查重**：搜有没有同一件事的 open issue 和 open PR，再看一眼最近关闭的 issue、合并的 PR（别重复做已经做完的）。
2. **已经有人认领、没有过期**：不要做。可以在 issue 下面补充信息、复现步骤、日志、线索（评论以「补充：」开头）。已经有 open PR 的同理，去 PR / issue 里补充。
3. **没有 issue 就新建**；开工时在 issue 里发一条认领评论：在做什么、打算怎么改、预计多久；Agent 在做要注明是 Agent。
4. **24 小时没有任何更新视为放弃**：别人可以发评论接手（写明接手原因）。自己还在做，就发一条进展评论续期。
5. **PR 必须关联 issue**：描述里写 `Fixes #N` / `Closes #N` / `Refs #N`。没有关联 issue 的 PR 由 Action 自动评论并关闭。

不用 GitHub 的 assign（很多人没有 triage 权限，也不需要）、不用 `/claim` 之类的命令，过期也不自动处理：一切以 issue 里的评论为准。

## 谁在做：以评论和 GitHub 账号为准

- **负责人就是评论作者的 GitHub 账号**。Agent 用它所属的那个人的 gh 账号发评论（不另开 bot 账号），这个人对 Agent 的认领负责；评论正文里不用再写名字。
- Agent 在做时，认领评论加一行 `Agent：是（<机器 id> / <分支或会话>）`：同一个人的几个 Agent 共用一个账号，靠这一行区分。
  机器 id 用配置里给机器起的 id，不写主机名。
- **只认 issue 里写的**：群里说一声、私聊约好、issue 的 assignee、标签都不算认领。
- 评论按第一行的前缀分类，方便人和 Agent 筛：`认领：`、`进展：`、`接手：`、`放弃：`、`补充：`。

### 什么算有效的认领

- 当前负责人是最新一条「认领：」或「接手：」评论的作者，并且之后这个账号没有发「放弃：」。
- **更新**：负责人在这个 issue 里的任何新评论，或关联 PR（描述里 `Fixes` / `Closes` / `Refs` 这个 issue 的 PR）有新的推送或评论。
- 从负责人**最后一次更新**算起 24 小时内，认领有效；超过 24 小时没有任何更新，视为放弃。
- 两个人几乎同时认领：先发的算。发完认领评论后再看一遍评论，自己不是先发的就删掉自己那条（`gh issue comment <号> --delete-last --yes`），去做别的。

## 评论模板

认领（Agent 照抄，人自己做就删掉「Agent」那一行）：

```text
认领：<在做什么，一句话>
打算：<怎么改、改哪几处>
预计：<多久，如「2 小时」「今天内」>
Agent：是（<机器 id> / <分支或会话>）
```

续期（还在做，24 小时内至少一条；有关联 PR 的推送也算更新）：

```text
进展：<做到哪了、下一步、新的预计>
```

接手（原负责人超过 24 小时没有更新）：

```text
接手：原认领 <时间>，最后一次更新 <时间>，已超过 24 小时
打算：<怎么改；原来的分支 / PR 有可用的进度就说明接着用>
预计：<多久>
Agent：是（<机器 id> / <分支或会话>）
```

放弃（做不下去或改做别的，主动说一声，别人不用等 24 小时）：

```text
放弃：<做到哪一步、剩下什么、分支 / PR 在哪>
```

补充（别人认领了，你有信息要给负责人）：

```text
补充：<复现步骤、日志、线索、相关代码位置>
```

## 命令（Agent 可以直接照抄）

在仓库目录里执行，`gh` 自动用当前仓库；`gh api` 路径里的 `{owner}/{repo}` 由 gh 自动填。时间都是 UTC。

### 1. 查重

```sh
# 同一件事的 open issue / open PR（标题、正文、评论都会搜）
gh issue list --state open --search "<关键词>"
gh pr list --state open --search "<关键词>"
# 一次搜 issue 和 PR
gh search issues "<关键词>" --repo "$(gh repo view --json nameWithOwner -q .nameWithOwner)" --state open --include-prs
# 最近关闭的 issue、合并的 PR、master 上的提交：别重复做已经做完的
gh issue list --state closed --search "<关键词>" --limit 10
gh pr list --state merged --search "<关键词>" --limit 10
git fetch origin && git log origin/master --oneline -i --grep "<关键词>" | head
```

查到同样意思的 issue（不管谁建的、标题怎么写）就在它上面认领，不另建一个；别人认领着的按「接手」的规矩来。确实没有才新建。

### 2. 看一个 issue 有没有人认领、过没过期

```sh
gh issue view <号> --comments
# 只看认领 / 进展 / 接手 / 放弃（时间、账号、第一行）
gh issue view <号> --json comments --jq '.comments[] | select(.body | test("^(认领|进展|接手|放弃)[：:]")) | "\(.createdAt)  \(.author.login)  \(.body | split("\n")[0])"'
# 关联到这个 issue 的 PR 和它们最后更新的时间
gh api 'repos/{owner}/{repo}/issues/<号>/timeline' --paginate --jq '.[] | select(.event == "cross-referenced" and .source.issue.pull_request != null) | .source.issue | "#\(.number)  \(.state)  \(.updated_at)  \(.user.login)  \(.title)"'
```

最新的「认领：/接手：」作者之后没有「放弃：」，且负责人的最后一条评论或关联 PR 的更新在 24 小时内：别做，去补充。

### 3. 建 issue

```sh
gh issue create --title "<一句话说清楚要做什么>" --label bug --body-file - <<'EOF'
## 现象 / 需求
<是什么、为什么>

## 复现
<步骤、日志>
EOF
```

需求加 `enhancement`，fix 加 `bug`，文档加 `documentation`。命令最后打印 issue 链接，末尾的数字就是号。

在网页上新建 issue 时可以选 `Bug report` 或 `Feature request` 表单（`.github/ISSUE_TEMPLATE/`），会自动带上 `bug` / `enhancement` 标签；字段名是英文，正文用中文写。也可以照常选空白 issue 或用上面的命令。

#### TestFlight 反馈

来自 TestFlight 的反馈（App Store Connect 里的崩溃报告、测试员的文字 / 截图反馈）建 issue 时要能按来源筛出来：

- **一律加 `testflight` 标签**（和 `bug` / `enhancement` 一起加）；没有加标签的权限时，在标题前加 `[TestFlight]` 前缀，有权限的人看到后补上标签。
- **先查重**：同一条反馈只建一个 issue。正文末尾带去重标记 `<!-- testflight-intake:<sha256(反馈 submission id)> -->`，
  建之前用 `gh search issues --repo <owner/repo> "testflight-intake:<hash>"` 查有没有建过；同一个崩溃原因已有 issue 时在原 issue 下「补充：」。
- **脱敏**：只放技术摘要和只保留符号的调用栈（去掉地址）、App 版本、系统版本、设备型号。不放测试员邮箱和身份、
  崩溃 / 安装 / 设备的各种标识、Bundle ID、内存地址和镜像 UUID、容器及个人路径、网络 / 电量 / 存储、语言和时区、精确时间、
  具体构建号和原始附件；原始日志只留在本机 0600 的文件里。正文末尾写一段「隐私处理」说明去掉了什么。
  例子见 #752、#1144。

```sh
gh issue create --label bug --label testflight --title "<一句话说清楚现象>" --body-file <脱敏后的正文>
```

### 4. 认领、续期、接手、放弃

```sh
gh issue comment <号> --body-file - <<'EOF'
认领：<在做什么>
打算：<怎么改>
预计：<多久>
Agent：是（<机器 id> / <分支或会话>）
EOF

# 发完再看一遍，确认最早的有效认领是自己（不是就 --delete-last --yes 删掉自己那条）
gh issue view <号> --json comments --jq '.comments[] | select(.body | test("^(认领|接手|放弃)[：:]")) | "\(.createdAt)  \(.author.login)  \(.body | split("\n")[0])"'

gh issue comment <号> --body "进展：<做到哪、下一步、新的预计>"
gh issue comment <号> --body "放弃：<做到哪一步、剩下什么、分支 / PR 在哪>"
```

接手用上面的模板，同样 `--body-file -` 发。

### 5. 提 PR 时关联 issue

```sh
# 提之前再查一次：issue 的评论、master 上有没有人已经修了
gh issue view <号> --comments
git fetch origin && git rebase origin/master

gh pr create --title "fix(ios): <英文标题>" --body-file - <<'EOF'
<English: what changed and why>

Fixes #<号>
EOF
```

- 合并：`scripts/hooks/merge-pr.sh <号>`，不直接 `gh pr merge`：它先用这台机器的私有值扫 PR 改的文件、提交信息和标题正文，通过才 squash 合并、删分支（`docs/testing.md`「隐私扫描」）。
- `Fixes #N` / `Closes #N`：合并后 GitHub 自动关闭 issue。`Refs #N`：只关联不关闭（一个 issue 拆成几个 PR、只修了一部分）。
- PR 的标题和正文用英文（squash 合并后就是 master 上的 commit），issue 和评论用中文。

## 自动关闭没关联 issue 的 PR

`.github/workflows/pr-needs-issue.yml`，PR 打开、编辑、重新打开时检查**描述**：

- **算关联**（大小写不敏感）：`fix` / `fixes` / `fixed` / `close` / `closes` / `closed` / `resolve` / `resolves` / `resolved` / `ref` / `refs`
  后面跟 `#号`（关键词后可以有冒号，如 `Fixes: #12`），或本仓库的 `owner/repo#号`；或者本仓库 issue 的链接（`https://github.com/<owner>/<repo>/issues/<号>`）。
- **不算**：写在代码块、行内代码、HTML 注释（PR 模板里的示例）里的；只写了 `#12` 没有关键词；别的仓库的 issue；PR 的链接；只在标题里写、
  或只在 PR 页面侧栏「Development」里手动关联的。
- 没有关联：留一条评论说明规则，然后关闭 PR。**作者编辑描述补上关联后会自动重新打开**，评论改成「已重新打开」；
  自动重新打开失败（比如分支已经删了）会再评论一条，这时补上后手动 Reopen（`gh pr reopen <PR 号>`）。
- 只重新打开 Action 自己关掉的 PR；被人关掉的 PR 补上关联也不会被打开。已经关闭的 PR 再编辑、仍然没有关联，不会重复评论。
- bot 账号（dependabot 等）的 PR 豁免。
- 改描述：`gh pr edit <PR 号> --body-file <文件>`，或者在网页上编辑。

### 维护

- 判断逻辑和流程测试：`node scripts/test/pr_needs_issue_test.mjs`（从 workflow 里取出 github-script 的脚本，用假的 GitHub 客户端跑几十种描述样例和关闭 / 重开流程），
  改完再跑 `actionlint .github/workflows/pr-needs-issue.yml`。
- 用的是 `pull_request_target`：跑的是默认分支上的那份 workflow，带写权限的 token，所以**绝不 checkout、不运行 PR 的代码**，
  PR 的标题、描述只通过 API 当数据读，不写进 `${{ }}`（会被注入脚本）。权限只给 `pull-requests: write` 和 `issues: write`。
- 也因为跑的是默认分支上的版本，改这个 workflow 的 PR 自己不会用到新逻辑，合并后才生效：合并前靠上面的测试。
