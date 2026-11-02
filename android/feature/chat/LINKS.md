# 会话内链接

`ChatScreen` 已接入 `ChatLinkHost`。网页从底部铺满，拖动标题栏或把手可停在六成，再下拉关闭；右侧按钮也能切换档位。六成时给会话和输入框留出空间，无遮罩，网页内容区保留自己的滚动手势。

裸链接、Markdown、用户消息、工具代码和交互正文统一使用 App 内入口。链接按项目约定在中文、空白处结束，去掉尾部标点和不成对的括号。网页新窗口回到当前 WebView；不自动唤起外部 App，仅菜单中的「在浏览器打开」主动外跳。

选区菜单提供「AI 修改」和「作为上下文」。修改提示连同选区交给现有 `MessageSender`；上下文在输入区可移除，出站消息落入 outbox 后才清理。引用使用已有 `<jarvis-context>` 协议并转义 XML，历史用户消息显示引用卡。未新增 HTTP 或 WebSocket 接口。

WebView 池随会话页面释放，使用 LRU 淘汰、系统 HTTP 缓存和 DOM 存储。列表停止滚动后预启动引擎并预加载近期链接，支持时调用预渲染 API，否则普通加载；后台页面暂停并在支持时静音。API 说明见 [Android WebViewCompat](https://developer.android.com/reference/androidx/webkit/WebViewCompat)。池容量和等待时间是实现配置，性能未实测。

## 当前 App 接线

`ShellChat` 与助手内嵌入口均使用已带 ChatLinkHost 的 ChatScreen，飞书会话挂 LarkLinkHost，外壳也为知识库等页面提供应用内链接入口。会话内选区上下文和 AI 修改已走现有草稿／发送器，随会话隔离；真实手势和完整链接覆盖仍待验收。

## 验证与边界

运行 `:feature:chat:compileDebugKotlin` 和 `:feature:chat:testDebugUnitTest --tests jarvis.feature.chat.ChatLinksTest`。两条关键测试覆盖中文链接边界、Markdown 保护、非网页协议拒绝，以及上下文和修改请求的 XML 往返。

当前 App 已完成上述接线，飞书文档也已有原生阅读器和网页降级；网页登录状态来自 WebView，不注入 hub 凭据。限制内嵌登录的 OAuth 站点需用户从菜单打开浏览器，尚未接 Custom Tabs。网页跨 iframe 的选区、真机拖动、键盘、深浅色截图和性能均未实测。
