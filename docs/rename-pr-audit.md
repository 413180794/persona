# #277：全量 open PR 改名残留审计记录

## 固定范围与结论

本记录归档 2026-11-03 06:41:09 UTC 查询到的 **25 个 open PR**。审计主线固定为 `09584f34fe6eb29f91ddc2a3c2a8df94017cb3ab`，每个 PR 使用下面的固定 head；这是当时的审计快照，不是此后所有开放 PR 的持续验收。

原只读审计逐项比较 PR 自身差异并模拟与固定主线的合并，发现 **11 项需要协调的迁移问题，涉及 9 个 PR**。堆叠 PR 的自身改动与继承内容分开归因：#713 继承 #566，#714 继承 #712，#764 继承 #713，不重复计算父 PR 的问题。

本次只归档分类结论与公开 PR 链接，不复制原报告中的作者账号、人名、旧个人 module 路径、具体用户数据、内部值或凭据。文档整理没有重新执行全量 PR 审计，也不代替各 PR 的功能评审、构建、真机或发布验收。

## 分类规则

- **自动 rename**：Git 已把原 Apple 文件映射到当前源码根；原 diff 中的旧路径不等于合入后重新创建旧目录。
- **保留身份**：旧 module、Intent / Control / ActivityAttributes、协议 / MCP 名称、序列化值、缓存键、服务身份和保旧生成链接按兼容契约保留，不要求旧品牌字符串归零。
- **需要迁移**：真实新增代码或文案会绕过新旧环境兼容、引用已迁走的源码 / import / 产物，或重新引入旧可见品牌。冲突中的旧分支内容需要正确取舍，但冲突模拟 tree 不是可发布代码。

分类可以同时出现；「未发现独立迁移新增」只说明此固定快照的改名范围，不代表该功能 PR 已通过质量验收。

## 全部 25 个 PR 的固定结论

以下 head 均为审计快照的完整 SHA。即使 PR 此后合并或更新，也保留原行，不用新 head 覆盖历史审计对象。

| PR | 固定 head | 分类 | 固定快照结论 |
| --- | --- | --- | --- |
| [#90](https://github.com/nwverse/persona/pull/90) | `ab8218172a0f3cb368fe61e403918ec1a20c1e42` | 需要迁移；保留身份 | 新 bridge 固定旧默认存储，自定义根安装不能正确找到程序；服务标签和日志身份保留。 |
| [#140](https://github.com/nwverse/persona/pull/140) | `54060f79fb33b147ce0277566b4b174690dcdc78` | 保留身份 | 仅 registry / lock 文档变化，未发现独立迁移新增；旧上下文不是新增残留。 |
| [#241](https://github.com/nwverse/persona/pull/241) | `3289b60adac145276cc8ac8c6a9eda3feff064d0` | 需要迁移；保留身份 | 新连接设备页有旧可见品牌，需改本产品提示；配对链接生成保旧。 |
| [#254](https://github.com/nwverse/persona/pull/254) | `47928b72a386ca42fcd3a8baa88b489417cc40fa` | 需要迁移；保留身份 | 新机器说明仍使用旧产品品牌；宿主和协议身份不改。 |
| [#291](https://github.com/nwverse/persona/pull/291) | `a28c7ff81e7f2622b80abf5a74a922f37b435683` | 需要迁移；保留身份 | 新生产环境入口只认旧前缀；保留系统类型的两份文件并存，且旧源码根重新出现。解决冲突时合并真实差异，不恢复旧根或改掉系统类型身份。 |
| [#292](https://github.com/nwverse/persona/pull/292) | `29433aae8420aac8402bd88110ab337e647677e9` | 需要迁移；自动 rename；保留身份 | 两个新增 Go 文件需使用当前仓库 import；Apple 新文件自动迁入新根，module / test / wire 身份保留。 |
| [#306](https://github.com/nwverse/persona/pull/306) | `3af57f29f2d9792955a83cf20bf2f07fe30dd7b1` | 保留身份 | 未发现独立迁移新增；旧助手识别是差异上下文，合并时保留主线 identity 兼容。 |
| [#415](https://github.com/nwverse/persona/pull/415) | `752e9133f733d2b5bd6c9e946248e400af6c4a67` | 保留身份 | 旧 imports 仅在待解决的冲突分支中；应保留当前仓库 import，未发现干净新增的旧根或公开环境入口。 |
| [#521](https://github.com/nwverse/persona/pull/521) | `3eda6479db71b3d978912de5a343638ea6e4c501` | 自动 rename；保留身份 | 新 Swift 文件自动迁到当前根，测试 module 保留；测试自有环境变量不自动视为生产兼容遗漏。 |
| [#530](https://github.com/nwverse/persona/pull/530) | `de257b96130cfde61b5f7b275082d3d750be6ac1` | 自动 rename；保留身份 | 新页面和测试自动迁移；既有演示字段和内容不按整行变化算新增产品文案。 |
| [#566](https://github.com/nwverse/persona/pull/566) | `e370599b32ffa6f137bef44ea27e6e701478a9f5` | 需要迁移；自动 rename | 新附件 Go imports 仍指向旧仓库路径；新增 Release guard 指向旧 binary，会漏检当前产物；Apple 源码自动迁移。 |
| [#581](https://github.com/nwverse/persona/pull/581) | `30974c724ebb4bac5aebce28860ec646fd28c034` | 保留身份 | 未发现独立迁移新增；临时文件名和兼容 CLI 不是可见品牌遗漏。 |
| [#679](https://github.com/nwverse/persona/pull/679) | `06b9f7abff712097fa8af008829bcf3756dfb2e2` | 自动 rename；保留身份 | 新测试自动迁移；MCP 工具和 module 保留，不改工具身份。 |
| [#712](https://github.com/nwverse/persona/pull/712) | `5d8ca27b76098fbea38161b73768ba004d45a42b` | 保留身份 | 当前源码根和 targets 保持，旧 module / bundle 身份保留；未发现独立迁移新增。 |
| [#713](https://github.com/nwverse/persona/pull/713) | `9ec806be75285f26bf367dc66bb81200acd5d654` | 需要迁移；自动 rename | 自身新增按钮仍使用旧可见品牌；imports / Release guard 问题继承 #566，不重复归因；新文件自动迁移。 |
| [#714](https://github.com/nwverse/persona/pull/714) | `fdc123586c9fc5e4fcab771c099f0b747864c071` | 保留身份 | 新配色源码在当前根，测试 module 正确保留；继承 #712 内容单列，未发现独立迁移新增。 |
| [#717](https://github.com/nwverse/persona/pull/717) | `48f8305f127f0711295ee1c480aaacc98cb149df` | 自动 rename；保留身份 | 新视频源码和测试自动迁移；内部临时缓存名和视频测试环境不视为生产品牌残留。 |
| [#721](https://github.com/nwverse/persona/pull/721) | `c84b005028cd066edc1dd6419c62559ec50ee5f2` | 需要迁移；保留身份 | 新 MCP 错误提示仍使用旧可见品牌，模拟合并无冲突但会重新引入；server / tool 身份保持。 |
| [#738](https://github.com/nwverse/persona/pull/738) | `93acf9c74e0a8764b8e5e663f46785a5b76770a2` | 保留身份 | 编译条件变化未改当前源码根和系统类型身份，未发现独立迁移新增。 |
| [#749](https://github.com/nwverse/persona/pull/749) | `d6182f90da10a0108e342c490bc35ae9412a219f` | 需要迁移（已由 #809 修复） | 新增当前文档引用指向不存在的源码根；审计期间已合入，后续用单条文档修正修复，不能当作待合 PR 阻塞。 |
| [#757](https://github.com/nwverse/persona/pull/757) | `d38f87ba8c957ad96c138791e3656640c3023154` | 保留身份 | 新运行根正确，健康 / MCP / schema 与 module 身份未因品牌改名改变，未发现独立迁移新增。 |
| [#764](https://github.com/nwverse/persona/pull/764) | `26999220bf6521540308b5317ceb5f18e4f30886` | 自动 rename；保留身份 | 自身无独立迁移新增；新加密文件自动迁移，相关 imports / guard / 按钮问题归父 PR；mock / env / wire 保留。 |
| [#765](https://github.com/nwverse/persona/pull/765) | `c33e81ddb00e7233964dece490270b61a5fb27cd` | 自动 rename；保留身份 | 原差异中的 UI 测试路径自动迁入新根；审计期间已合，未发现独立迁移新增。 |
| [#781](https://github.com/nwverse/persona/pull/781) | `ec6e13c39bf525706630d995acfdaa17e99cf655` | 保留身份 | 新业务 / mock 代码未重新引入旧 import、运行根、公开环境入口或产品品牌。 |
| [#784](https://github.com/nwverse/persona/pull/784) | `579391134cc974d27adbe0cbfccccf14080541ed` | 保留身份 | 新 Go imports 使用当前仓库；协议字段是业务增加，不属于机械改名。 |

## 快照后的处理状态

- #749 的非链接源码引用漏项已由 [#809](https://github.com/nwverse/persona/pull/809) 修复并合入，merge 为 `29e7249c1b9857ba58e3453fa285543b74d5d75a`。定点检查验证了旧引用失败、修正后实际文件存在；原 Markdown 链接审计没有覆盖该代码字面量，不能用链接统计代替这项检查。
- 原快照扣除已修文档项后，涉及 **8 个 PR、10 项问题**的迁移证据已经补充到各 PR；这描述原问题范围，不是当前仍未修的数量。新增 head 的增量复验见下表，仍需原负责人协作。补充证据或部分修正不等于整个 PR 已修复、已合入，也不授权合并无关功能。
- 整理时复查 PR 元数据，没有重复全量审计。#90、#241、#254、#291、#292、#721 的当前 head 与上述快照相同；#566 和 #713 的 head 已更新。leader 已对这两项进行只读增量复验，结果在下面单独追加，不覆盖原固定结论，也不写成「最新 head 全部未变」。

| PR | 整理时查询到的新 head | 处理边界 |
| --- | --- | --- |
| [#566](https://github.com/nwverse/persona/pull/566) | `ff5c6e5a37ddc3f44b04b7fedc7465e36b636734` | 新附件 imports 已改为当前仓库，Release guard 的旧 binary 路径已修，并加入该 PR 的新 marker；但完整清单仍缺 `showConnectionCycle` 和 `showToasts`，必须 rebase 并保留主线完整 guard。属于部分修复，不能全判已修。 |
| [#713](https://github.com/nwverse/persona/pull/713) | `a64a1b4d4af77b4286c798b8f9a87f33ed3c527e` | 当前源码路径已存在，但新增按钮仍使用旧可见品牌；该文案问题未修。父 PR 与自身业务质量仍分别验收。 |

这不是新的开放 PR 数量统计。原 25 行是固定快照；后续新开、合并或更新的 PR 不能据此推断已完成审计。

## 08:59 固定快照的增量复核

本节追加 2026-11-03 **08:59:00.026551 UTC** 的增量审计，主线固定为 `0e02f0f7fc6aec48cc73197642917c382294ae81`。当时有 **26 个 open PR：7 个新 PR、4 个 head 更新、15 个 head 未变**；原 25 项中另有 6 项退出 open 集合，不据此推断它们全部已合并。审计完成时于 **09:02:59.325821 UTC** 再次核对元数据，主线、集合和 head 均未变化。

对应审计仅复核 **11 个 delta 对象**，没有重审 15 个未变化 PR；模拟合并为 9 项无冲突、2 项有冲突。**未发现新的独立迁移问题**，但 #764 仍承载父分支的旧按钮文案，需要协调。这个结论不抹去未变化 PR 的既有问题，也不代表所有开放 PR 已通过功能、构建、截图或发布验收。文档车道只归档既有证据，没有重新执行增量审计。

### 4 个更新 head

下表追加新证据，不改原 25 行或前一轮部分修复表；「已修」仅指注明的迁移项，不代表整个 PR 已合入或通过业务验收。

| PR | 08:59 固定 head | 增量结论与边界 |
| --- | --- | --- |
| [#566](https://github.com/nwverse/persona/pull/566) | `40d0fec99ded5ba66a5f4ed44277d09574077a7b` | 原附件 imports 与 Release guard 迁移项已修：使用当前 Go module，guard 读取 `Persona` binary，完整保留固定主线的 190 个 marker，并新增 `showLarkAttachmentDraft`；`showConnectionCycle`、`showToasts` 未丢失。模拟合并无冲突，未做附件功能验收。 |
| [#581](https://github.com/nwverse/persona/pull/581) | `bd556eab5f14d9e70364bc771595d1de67013a1b` | 未发现独立迁移新增。当前 Go module、运行根与公开环境入口未回退；旧临时文件名和兼容 CLI 保留，不据此判断授权功能已通过。模拟合并无冲突。 |
| [#713](https://github.com/nwverse/persona/pull/713) | `21242d10f3d2a10d4a78828f5c145b3ba9f8b24d` | 原按钮文案已修为「让 Persona 总结」，继承的 imports 和完整 guard 保全。聊天页和经验文档的模拟合并有冲突；未发现新独立迁移问题，但不能宣称可直接发布。 |
| [#764](https://github.com/nwverse/persona/pull/764) | `070eb0192249283eddcbe262dd624e9bed80bbee` | 自身加密增量未新增旧 module、根目录或公开环境入口问题，imports 和完整 guard 保全；仍继承父分支的「让 Jarvis 总结」按钮，尚未带入 #713 修正。聊天页和经验文档的模拟合并有冲突，该残留不重复归因为自身新问题。 |

#764 的固定 head 中，`apple/Persona/Views/Inbox/Lark/LarkChatView.swift:103` 仍为 `Label("让 Jarvis 总结", systemImage: Theme.Icon.ai)`；其 base 和旧快照也有同样文案。需要带入 #713 的修正，或解冲突时保留「让 Persona 总结」。这里只确认分支仍承载旧文案，**不能根据冲突 tree 断言最终产品必然引入该错误**；内部方法、module 和系统身份不随 label 改名。

对应审计还用匿名 `.app` fixture 验证了 #566、#713、#764 的完整 guard 条件：三个 head 均无主线 marker 缺项，新 marker 均被检测到。这不是 release 脚本、真实产物或上传验证；#713、#764 的冲突 tree 也不是可发布版本。

### 7 个新 PR

| PR | 08:59 固定 head | 增量结论与边界 |
| --- | --- | --- |
| [#828](https://github.com/nwverse/persona/pull/828) | `22b445e873e3917e9ed7d9b7adbbdb1f9fbbe33b` | 当前 Apple 源码根，排序状态未盲改 module、持久化键或系统类型，guard 无减项；未发现独立迁移新增，未做排序功能或截图验收。 |
| [#837](https://github.com/nwverse/persona/pull/837) | `2cc85f9ce08882adf56d6f99f64730f5d4914a5f` | HOME-only 运行根恢复；parser hash 与既有定点复验版本一致，复用准确版本的证据，未重复执行脚本测试。未发现独立迁移新增，不扩展为完整 parser 或真实升级验收；快照后的实际合入状态见下节。 |
| [#860](https://github.com/nwverse/persona/pull/860) | `4bc2a2cc38f0506e529caec8ffaa85089c3e2640` | 当前 Go module，botmux 自有的上游环境与 cookie 身份不属于 Persona 前缀遗漏，可见来源为 Persona；未发现独立迁移新增，未运行宿主、消息投递或查询真实会话。 |
| [#863](https://github.com/nwverse/persona/pull/863) | `0589a9a130133c8ee000da31e3b5eb7549d233ce` | 安装文档未引回旧源码根；旧 Xcode 工程入口是保留的生成工程 alias；未发现独立迁移新增，不验证安装效果。 |
| [#869](https://github.com/nwverse/persona/pull/869) | `2d4e1eaf9eb9b28948f48ac20ecc5a2b1c59e1b3` | Android 键盘局部改动未改 package、系统组件、序列化或缓存身份；未发现独立迁移新增，未编译或截图。 |
| [#871](https://github.com/nwverse/persona/pull/871) | `ceb9d95e4df1f6be11fa7141dd72356779a5c252` | Web 局部渲染未改 scheme、持久化身份、运行根或公开环境入口；未发现独立迁移新增，未跑 Web 测试、构建或浏览器验收。 |
| [#872](https://github.com/nwverse/persona/pull/872) | `76e97efd47c7785d347e4e9eba64ec803cc2e280` | 回复后缀与提示词说明的局部变化保留新旧标记，不改产品名、工具身份或权限规则；未发现独立迁移新增。本轮审计未运行回复测试、golden 或飞书请求。 |

7 个新 PR 与固定主线的模拟合并均无冲突，只说明固定 object 的合并结果，不是 CI 或运行验收。

**15 个未变化 PR：** #90、#140、#241、#254、#291、#292、#306、#415、#521、#679、#712、#714、#717、#721、#757。只核对 head 元数据；#90、#241、#254、#291、#292、#721 等原迁移结论仍保留，本轮不重新判断其状态。

**6 个退出 open 集合的 PR：** #530、#738、#749、#765、#781、#784。仅记录集合变化，未逐项重审终态或实际 merge；#809 的路径修正和 #849 的历史审计归档继续保留，不冒充本轮生产复验。

## #837 实际合入后的 EnvironmentFile 支持

[#837](https://github.com/nwverse/persona/pull/837) 已合入，merge 为 `62039ebc4d17285a4ffd39e3631e96e18e65c70d`。这是 08:59 快照之后核对的实际状态，不覆盖上表中的固定 head。

`scripts/persona_identity.py` 现在支持从 `EnvironmentFile=` **只提取 `PERSONA_HOME` / `JARVIS_HOME`**：按文件顺序覆盖 unit 环境，支持可选文件、文件名模式和空赋值重置，再应用 `UnsetEnvironment=`。文件只有其他设置而没有 HOME 时，不再仅因外部文件存在而拒绝恢复旧默认根；显式新旧 HOME 的优先级和定义为空的行为保持。必需文件缺失或根字段解析错误仍停止，不因无法恢复而切到另一份空存储。不执行文件内容或 shell 展开，不返回、记录或用其他字段认证。完整契约见 [运行目录恢复说明](rename.md#部署脚本按服务恢复运行目录)。

**对应代码车道的证据，不是本文档车道重跑：** 最终固定 head `2cc85f9ce08882adf56d6f99f64730f5d4914a5f` 记录 128 项本机脚本测试、16 项正式匿名专项测试通过；全部 9 项独立报告的问题均有原输入在修复版本上的通过证据，包含 UTF-8 字节与 Unicode codepoint 转义的最终补洞。只说明已审覆盖内没有剩余确认问题，不宣称完整 systemd 兼容。CI 的自建 runner 检查在有时限等待中始终排队、没有执行；合并使用已授权的本地验证 fallback，**CI 不算通过**。未使用真实 EnvFile、生产配置、数据库、部署或上传。

## 服务端测试安全边界

[#802](https://github.com/nwverse/persona/issues/802) 记录了服务端测试启动器继承生产运行根的隔离问题；后续 [#812](https://github.com/nwverse/persona/pull/812) 已修复启动器隔离，merge 为 `81b237e397ed7b83ee06dedf3f6320ebf7d3af01`。对应车道已记录真实 Postgres 复验通过，但先前已有实际生产文件删除证据，**完整影响范围仍未知**。不能因后续隔离修复和复验通过，就推断先前生产状态未受影响或已恢复。

本记录只写安全边界和未完成状态，不复制任何生产数据、受影响对象明细、实际数量、地址或凭据。文档车道不运行真实服务端测试、部署或上传，也不访问生产根以调查影响。

另有数据库 URL 的 query 参数可能覆盖测试路径的隔离边界问题，仍由发布车道处理。真实服务端测试、部署和上传继续暂停；#837 的 parser 合入不解除这项暂停，也不证明完整事故影响已恢复。

## 发布状态与下一步

- **1045 发布失败，不能计为成功或最终验收。最终发布尚未完成。**
- 原固定快照审计及上述 11 项 delta 复核已完成，但未修迁移项、后续 PR 协作和发布冻结是不同阶段。**最终冻结尚未开始**；不能把审计完成写成所有问题已修或整个 #277 完结。
- 原负责人在各自 PR 中处理迁移项，保留已正确迁移的主线内容；不要为收尾合并无关功能。
- 本文档 PR 只归档和索引，保持交接边界，不合并业务 PR、不部署或上传。

兼容规则与保留身份清单见 [改名迁移说明](rename.md)。
