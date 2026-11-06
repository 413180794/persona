# 路由接线说明

本模块只定义目标、解析和栈恢复，不负责联网、发送分享内容或执行配对。所有入口调用 `Router.parse(RouteInput)`，成功返回 `ResolvedRoute(tab, key, pairingLink)`。

`ChatRoute(machineId, sessionId, kind, source)` 中，`kind` 区分 `Assistant` 与 `Agent`，分别落到 Persona、Agents。`source` 保留收件箱条目、飞书会话或交互请求的身份。旧的双参数构造和序列化数据仍可读取；未提供 `kind` 时，`Router` 通过构造参数中的仓储查询函数补齐身份，元数据未就绪就返回 `null`，不能按当前 tab 猜测。调用方在元数据到达后重试待打开的旧目标。

| 入口 | 输入 | 落点 |
| --- | --- | --- |
| 正式深链 | `DeepLink("persona://open?machine=…&session=…")`，可加 `kind=assistant` 或 `kind=agent` | 对应身份的 tab 和对话 |
| 配对深链 | `DeepLink("persona://pair?…")` | 设置 tab 的 `PairingRoute`；原始链接通过临时字段 `pairingLink` 传入配对确认页 |
| 通知 | `Notification(sessionPath, kind, requestId)` | 对应对话；有请求 ID 则进入 `InteractionRoute` |
| 分享后查看对话 | `Share(ChatRoute(…))` | 分享目标所属 tab 的对话 |
| App 内跳转 | `Internal(NavKey)` | 对话按身份分流；收件箱条目和飞书会话进入收件箱；设置子页进入设置 |

`persona://open` 还支持 `target=inbox&item=…`、`target=lark&chat=…`、`target=request&machine=…&session=…&request=…`、`target=settings&page=diagnostics`、`target=tab&tab=inbox`。参数值采用 URL 百分号编码，原始 `+` 保留为加号；重复参数或未知目标返回 `null`。配对参数与传输安全策略仍交给现有 `PairingInput` 校验，解析路由不会直接配对，配对凭据不会放进可序列化的导航键。

导航调用 `resolved.restore(stacks)` 后选择返回的 tab。已有目标会弹回原位置，清理其上的页面；来源改变不会增加同一会话的栈项，也不会替换已有键导致页面状态丢失。误放在其他 tab 的同一目标会迁走。tab 根目标只切 tab，保留该 tab 的返回栈。`ConnectionSettingsRoute` 兼容入口统一落到诊断页。

# 留给接线车道

- `MainActivity.handle`：将正式深链和通知 extras 转为 `RouteInput`，替换分别拆解 URI 和会话路径的逻辑。冷启动保留待处理输入，等配对恢复、导航栈和所需会话元数据就绪后解析；成功打开才消费。
- `Root.kt`、`AppNavigator.kt`：提供助手主对话、旁支和会话列表的身份查询；内部 `navigate` 也调用同一个解析函数，使用 `restore` 更新每个 tab 的栈，替换当前按所选 tab 兜底和仅比较栈顶的逻辑。
- `ShareActivity`：查看已分享目标时传递确定的会话身份，通过 `Share` 解析；内容读取、上传及发送仍由分享流程负责。
- `:app` 的 `entryProvider`：接入 `InboxItemRoute`、`LarkChatRoute`、`InteractionRoute`、`PairingRoute`、`SettingsPageRoute` 的页面。设置子页包含诊断、后台准备、通知、手机数据和风格；目前 feature 的设置页仍用内部字符串状态，接线时需提供对应页面入口。请求页通过机器、会话及请求 ID 获取数据。
- `:app/src/debug`：新增 debug Activity / Intent 适配，调用下表的 `Router.parseDebug(Map<String, String>)`。解析代码仅在本模块 `src/debug` 编译，release 不提供该函数或参数字符串；Activity 和 manifest 声明也必须仅在 app 的 debug source set。

| debug extra | 值 |
| --- | --- |
| `debug.openTab` | `persona`、`agents`、`inbox`、`settings` |
| `debug.openSession` | `<machine>/<session>` |
| `debug.openRequest` | 请求 ID，另传 `debug.session=<machine>/<session>` |
| `debug.openPairing` | `true` 或完整 `persona://pair` 链接 |
| `debug.openDiagnostics` | `true` |
| `debug.conversationKind` | 会话或请求可附带 `assistant` / `agent`，省略时查仓储 |

一次只允许一个 `debug.open…` 目标。debug 适配最终也调用 `Router.parse`，与正式入口共用目标解析及恢复规则。
