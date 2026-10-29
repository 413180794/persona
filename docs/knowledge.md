# 知识库（`~/.jarvis/data`）

Jarvis 和 AI 分身要「代替我上班」，前提是它们知道的和我一样多：谁是谁、哪个群在干什么、每个项目进展到哪、哪些事和我有关。
这篇讲 hub 怎么把飞书和邮件里的东西沉淀成一份**文件为主、数据库做索引**的知识库，以及 Jarvis、AI 分身、Agents 怎么共用它。
（2026-10-30 用户定的方向：所有数据最终沉淀在 `.jarvis` 目录；文件为主，库做索引；文字保留 365 天，附件 30 天。）

## 为什么是文件

- Claude Code 读文件、grep 最顺手：助手、AI 分身、herdr 里的 Agent 不用学新接口，`rg 关键词 ~/.jarvis/data` 就能查。
- 人也能直接看、直接改（改了的 `summary.md` 下次压缩会被当成已有上下文）。
- Postgres 只放「要排序、要筛选」的索引（`kb_entities`：类型、名称、重要级、相关性、标签、路径、更新时间），丢了可以从文件重建。

## 目录

```
~/.jarvis/data/
  README.md                 # 本目录的说明（hub 生成），给 AI 看
  chats/<chat id>/          # 一个会话一个目录：飞书 lark-<feedId>，邮件 mail-<账号>-<对方地址>
    meta.json               # 名称、类型（dm / group / bot / mail）、成员、关联的人和项目、打标、归档游标
    messages/YYYY-MM-DD.md  # 全量文字，一天一个文件，追加写；365 天后删
    attachments/YYYY-MM-DD/ # 图片、文件；30 天后删
    summary.md              # 压缩后的上下文（AI 维护，滚动更新）
  people/<person id>/       # 一个人一个目录：飞书 lark-<userId>，邮件 mail-<地址>
    meta.json               # 名字、所在的会话、关联项目、打标
    summary.md              # 这个人是谁、和我什么关系、最近在跟我聊什么
  projects/<slug>/          # 一个项目一个目录
    meta.json               # 名称、类型（short 短期需求 / long 长期使命）、状态、成员、相关会话 / 文档 / 会议 / Agent 目录
    summary.md              # 目标、现状、下一步、风险、我的角色
    log.md                  # 时间线：每次有新进展追加一条（日期 · 来源 · 一句话）
  docs/<id>.json            # 文档 / 会议 / 日程：标题、链接、时间、打标、关联项目
  meetings/<id>.json
  events/<id>.json
```

## 打标

每个会话、人、项目、文档、会议、日程都有三样，由 AI 在压缩时给出，人可以改：

| 字段 | 取值 | 含义 |
| --- | --- | --- |
| `importance` | `P0` / `P1` / `P2` / `P3` | 重要级：P0 关键（老板、核心项目、线上事故）、P1 重要、P2 一般、P3 可忽略 |
| `relevance` | 0–100 | 与我相关性：直接找我 / 我负责 → 高；只是在群里看到 → 低 |
| `tags` | 字符串数组 | 不预设，AI 自动生成（2–6 个短词，复用已有标签优先，`kb_tags` 给出现有标签和次数） |

## 流水线（hub 后台，`server/internal/kb`）

1. **归档**：每 2 分钟（飞书实时事件来了 5 秒后）看飞书「消息」列表最近活跃的 100 个会话，列表时间比上次归档的新才去读，
   按游标（`afterPosition`）增量拉，第一次见到的会话从最新 100 条开始；追加到当天的 `messages/*.md`（时间按 hub 机器的时区）。
   图片 / 文件 / 富文本里的图下载到 `attachments/<日期>/`（单个 20 MB 以内），行里留 `[附件](相对路径)`；消息里的飞书文档链接记进 `docs/`。
   话题群只读最新一页的话题。邮件每 6 分钟按账号读收件箱最新 40 封里 UID 比上次大的，每轮最多读 10 封全文（其余用摘要），
   记到 `mail-<账号>-<对方地址>` 会话里；游标在 `mail-cursors.json`。
2. **压缩**：会话攒够 50 条新消息，或安静了 10 分钟，就把旧的 `summary.md` + 新消息（最多 2.4 万字，留最新的）交给小模型，
   得到新的摘要（固定小节：这是什么 / 进展 / 待办和未决 / 关于我）、一句话、打标、提到的项目和人；写回 `summary.md`、`meta.json`。
   每轮最多压缩 8 个会话，相关性高的先。私聊 / 邮件的打标同步给对方这个人；人的 `summary.md` 不调模型，由 meta 和私聊上下文拼出来。
3. **演化（evo）**：压缩结果里提到的项目合并进 `projects/`（名字或别名去掉标点、大小写后相同就并，没有就新建），有进展就追加 `log.md`
   并标脏；每轮最多重写 3 个脏项目的摘要（旧摘要 + 最近 40 条时间线 + 最多 8 个相关会话的摘要）。人、会话、项目之间的关联写进各自的
   `meta.json`（双向）。长期项目（long）不会被改回短期。
4. **文档 / 会议 / 日程**：每 30 分钟读飞书日历（昨天到 8 天后，忙碌的日程）和最近 30 场会议，补 10 个文档的标题（Markdown 导出的第一行），
   新的和变了的每 40 条一批交给模型打标、挂到项目上。
5. **清理**：每天一次，删 365 天前的 `messages/*.md`、30 天前的 `attachments/*`（天数在 `hub.json` 的 `kb` 里改）。

## 谁在用

- **Jarvis（主对话和其他对话）**：CLAUDE.md 里写明目录结构和「让上下文流动」的要求；MCP 工具 `kb_search`（全文）/ `kb_list`（排好序的列表）/
  `kb_get`（档案 + 最近 7 天消息）/ `kb_update`（建项目、改档案、打标、建关联、记时间线；手动打的标会锁住，压缩不再覆盖）；
  实时快照里带最重要的 6 个项目、8 个会话。
- **主对话**：hub settings `assistant.main` 记着它的会话 id（`POST /api/v1/assistant/conversations` 带 `main: true` 时设上，
  `PUT /api/v1/assistant {"main":"-"}` 清掉）；node 起进程时把会话 id 带给 `assistant.context`，主对话的快照开头多一句「这是主对话」。
- **AI 分身**：交给 Agent 的提示词末尾附上这个会话和它前 3 个项目的档案（`<jarvis-context>`），并要求它把新结论用 `kb_update` 写回项目时间线。
- **Agents**：项目 `dirs` 记着 `<机器>:<路径>`；`GET /api/v1/kb/projects/<slug>` 带回在这些目录里跑的 Agent。App 里从项目开 Agent、
  问 Jarvis 时把项目档案作为上下文带上。

## 配置

`hub.json`：

```json
"kb": {
  "enabled": true,
  "text_days": 365,
  "attachment_days": 30,
  "ai": { "base_url": "<网关地址>", "api_key_file": "<key 文件>", "model": "<模型>" }
}
```

`kb.ai` 不写就用 `inbox.triage_ai`，再没有就用 `ai`。`enabled: false` 整个关掉。

## App 接口

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/kb/{kind}?tag=` | 排好序的列表（`items`）和常用标签（`tags`）；kind 是 projects / chats / people / docs / meetings / events |
| `GET /api/v1/kb/{kind}/{id}` | `detail`（meta、summary、项目时间线、会话最近 7 天消息）；项目还带 `agents` |
| `PUT /api/v1/kb/{kind}/{id}` | 改（字段同 `kb_update`）；项目不存在就新建 |
| `GET /api/v1/kb-search?q=&messages=0` | 全文搜索 |
