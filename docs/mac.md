# macOS App

原生 macOS 版 Jarvis（`ios/` 里的 `JarvisMac` target）：和 iOS App 同一套逻辑层（`Shared/`、`Jarvis/App`）和内容视图，
外面是 Mac 自己的壳（`ios/Mac/`）：窗口、侧边栏、工具栏、菜单、快捷键、设置窗口、菜单栏。目标是 iOS App 的全部功能一个不少，
交互按 macOS 原生的方式做，滚动和动画 60 fps 起（ProMotion 屏 120 fps）。

## 做法

- iOS 的代码不为 Mac 改样子：iOS 专用的东西用 `#if os(iOS)` / `#if canImport(UIKit)` 隔开；SwiftUI 里 iOS 专用的修饰符
  （`navigationBarTitleDisplayMode`、`.topBarTrailing`、`.insetGrouped`…）在 `ios/Mac/Compat/` 里给同名的 Mac 版本，页面代码照常写。
- UIKit 写的视图（终端、代码行、等宽文本、图片查看、链接页、长按菜单…）在 Mac 上用 AppKit 重写同名类型，放 `ios/Mac/`。
- Mac 上没有的能力换成 Mac 的做法：灵动岛 / 实时活动 → 菜单栏；Apple Watch 同步、健康数据 → 不做（说明原因）；拍照扫码 → 粘贴配对链接。

## 验收清单

下面是 iOS App 的全部功能，Mac 版逐条对上（做完一条勾一条）。


## 0. App shell / navigation
- [ ] 4 tabs with badges: Jarvis (pending assistant questions), Agents (pending interactions, excluding muted), 收件箱 (inbox action count), 设置 (「体验」 badge in demo) — Jarvis/Views/RootView.swift
- [ ] Toast overlay (success / info-blue / error), haptics (success/warning/error) — Views/RootView.swift (ToastOverlay), App/AppModel.swift
- [ ] Rebuild whole tree when style/persona changes, keep tab + open chat — Views/RootView.swift
- [ ] Width-driven layout: compact = single NavigationStack, regular = NavigationSplitView (list | detail), shared path, `.splitDetailPage()`, sidebar width 340–480, SidebarTopFill — Views/SplitNavigation.swift
- [ ] Readable width cap (chat 760pt, welcome 560pt) — Views/Chat/ChatView.swift, Shared/Design/Theme.swift
- [ ] URL handling: `jarvis://pair?...` pairing, `jarvis://open?machine=&session=` (Live Activity) — App/JarvisApp.swift
- [ ] Scene phase: reconnect + refresh on active, disconnect socket on background — App/JarvisApp.swift
- [ ] Remember last tab / last open chat / reading position across cold start (`chat.read.<session>`) — App/AppModel.swift, Views/Chat/ChatView.swift
- [ ] DEBUG launch args (-tab, -openSession, -openPane, -openTerminal, -lark-connector, etc. list in docs/ios.md) — App/JarvisApp.swift + views

## 1. Jarvis tab (assistant)
- [ ] Headless Claude assistant conversations (main conversation `assistant.main` + side branches); first launch opens main — Views/Assistant/AssistantView.swift, App/AppModel.swift (assistantMain, refreshAssistant)
- [ ] Home: greeting + available devices + 6 suggestions; launcher/model chip list; StarterComposer (or BeautifulStarterComposer) with photos — Views/Assistant/AssistantView.swift (AssistantHome, StarterComposer, StarterModelChip, AssistantToolsRow)
- [ ] First message shows bubble + 「正在准备」 immediately, session starts in background; warm assistant — AssistantView.swift, AppModel.warmAssistant
- [ ] Thinking hidden; tool calls folded into 「工具 N · 电脑、iPhone…」 — Views/Chat/ChatLines.swift, ChatView.swift
- [ ] Floating top bar with fog, center persona dot + name capsule; connection state replaces name — Views/Assistant/AssistantTopBar.swift (AssistantTopBar, TopFog)
- [ ] Avatar menu: 知识库, 详细信息 (profile), 回到主对话, 拷贝目录 / 拷贝信息 / 拷贝会话 ID (「✓ 已拷贝」 3s), 启动器 submenu (switch launcher, applies via --resume next message), 会话信息, 中断, 连接详情 — AssistantTopBar.swift
- [ ] Left button: history sidebar; right button: new chat (disabled on new-chat page) — AssistantTopBar.swift
- [ ] Sidebar: search, new chat, Jarvis card (online/offline + machine → profile), 定时任务 / 知识库 tiles, 置顶 (main pinned, long-press pin, `assistant.pinned`), day groups 今天/昨天/最近7天/更早 (8 + 查看全部), row = title + preview + time + status dot (need-you red / unread blue / working spinner), long-press 拷贝会话 ID / 结束 / 置顶, 设置 row in drawer — Views/Assistant/AssistantSidebar.swift
- [ ] Drawer gesture: pan anywhere, 45° rule, yields to horizontally scrollable content, velocity/half settle, page card scale/offset, tab bar fade (UIKit host) — AssistantSidebar.swift (DrawerPan, DrawerPageMotion, DrawerSidebarHost), Views/Assistant/DrawerProbe.swift
- [ ] Regular width: sidebar = split view left column, toggle button — AssistantView.swift, SplitNavigation.swift
- [ ] Jarvis avatar (mask face, eyes move when working) — Views/Assistant/JarvisAvatar.swift
- [ ] Profile page: identity, machine, online/working, 「接下来」 3 items, 主动做事 (tasks, run history), 了解你 (关于我, 记忆), 设置 (launcher, 管理启动器), capabilities list — Views/Assistant/AssistantProfileView.swift (AssistantProfileView, AssistantUpcomingRow, AssistantRunsView, AssistantRunRow)
- [ ] 「交给 Jarvis」 from other pages sends `<jarvis-context>` into main conversation — AppModel.askJarvis

## 2. Agents tab (dev list)
- [ ] Always-visible search; quick filters 全部/需要你/未读/工作中 with counts; snapshot (`kept`) semantics — Views/Chats/ChatsView.swift (QuickFilterBar, AgentsList)
- [ ] Grouping menu: 按项目 / 按状态 / 按机器 (`DevGrouping`, remembered); one rank order — ChatsView.swift, Views/Chats/AgentListOrder.swift, Views/Chats/AgentsMenus.swift (AgentsListMenu)
- [ ] Card vs compact list (`dev.compact`); compact rows on one scale — ChatsView.swift (SessionRow), Views/Chats/CompactList.swift
- [ ] 「需要你处理」 cards with quick-action buttons (card mode) / CompactInteractionRow — Views/Interactions/InteractionViews.swift (InteractionPreviewCard), CompactList.swift
- [ ] Pinned agents, pinned spaces, busy / just-done / muted-running / idle spaces (folded), 「还有 N 个空闲」 — ChatsView.swift (IdleFoldRow, QuietSpaceRow, SpaceHeader, StateCounts)
- [ ] Fold/unfold spaces and section headers (`dev.folded`), 全部折叠/全部展开 — ChatsView.swift, AgentsMenus.swift
- [ ] Mark all read (filter + per-space, with confirm) — ChatsView.swift, App/AppModel.swift (markSeen)
- [ ] Swipe: left = pin / interrupt, right = close; long-press row menu (rename, mute/unmute, 交给 Jarvis, 拷贝恢复命令, 关闭 Agent…, stop loop) — ChatsView.swift, AgentsMenus.swift (AgentRowMenu)
- [ ] Close agent confirmation sheet — DesignSystem/Components.swift (CloseAgentSheet, CloseAgentConfirmation)
- [ ] Space header ⋯ menu: 打开空间, 置顶, 屏蔽/开启通知, 重命名空间, 全部标为已读 — AgentsMenus.swift (SpaceHeaderMenu), ChatsView.swift (SpaceMenuItems, RenameSpaceAlert)
- [ ] Rename agent (herdr tab + Claude session name) — ChatsView.swift (RenameSessionAlert), AppModel.renameSession
- [ ] Pins / mutes synced with hub (/api/v1/pins, /mutes), optimistic, per-agent override of space mute — App/PinsStore.swift
- [ ] Badges: 「⟳ 下次 12:20」 loop, 「⚙ 后台 N」, 「👥 子 Agent N」, muted 🔕 styling — ChatsView.swift (SessionRow)
- [ ] Machine filter menu, 「机器监控」 entry, 新建会话 button — ChatsView.swift (TopBar), AgentsMenus.swift
- [ ] Connection status in title slot — Views/ConnectionTitle.swift
- [ ] Pull to refresh — ChatsView.swift
- [ ] Destinations registry (SessionKey, space, machine pages) — ChatsView.swift (AgentsDestinations)

## 3. Spaces & machine pages
- [ ] Space page: agents in herdr tab order (SlotCard), machine button, menu (rename, mute, mark read, 新标签 / 在这个空间开新标签) — Views/Chats/SpaceView.swift (SpaceView, SlotCard)
- [ ] Sibling tabs strip in chat header (state-colored capsules, swipe, ⋯ list with red dot, 「空间 X 的所有 Agent」) — SpaceView.swift (SiblingTabs, SiblingList)
- [ ] Machine spaces page: online state, counts, busy projects with latest line, idle projects, loose agents, ⓘ details — Views/Chats/MachineSpacesView.swift (MachineSpacesView, BusySpaceRow)

## 4. Chat page (agent conversation)
- [ ] Message rendering: user bubble, assistant markdown (MarkdownUI `.jarvis` theme), thinking row, peer 「来自 名字」 card, pending/sending bubble (retry/delete), merged queued bubble, sent photos, attached images — Views/Chat/MessageViews.swift (UserBubble, PeerMessage, PendingBubble, MergedUserBubble, SentPhotos, AttachedImages, AssistantText, ThinkingRow, NoticeRow)
- [ ] Markdown tables fitted / horizontal scroll — Views/Chat/MarkdownTable.swift (FittedTable)
- [ ] Line model: one turn → message / work line / answer; work line counts & duration; split merged messages — Views/Chat/ChatLines.swift, ChatView.swift (WorkLine, WorkStepView, WorkCardSlice)
- [ ] Work trace expand/collapse in place (timeline style for glass, card style for others), 「收起 N 步」, pin tapped row under finger — ChatView.swift, Views/Chat/ChatPin.swift
- [ ] Tool views: tool groups, tool call rows, detail, Bash command/output, full output sheet, TodoWrite list, base64 images, folded work row — Views/Chat/ToolViews.swift
- [ ] Unified DiffView (Edit/MultiEdit/Write + Codex unified diff), +N −N, 40-line truncation, horizontal scroll — ToolViews.swift (DiffView), Views/Session/GitPane.swift (DiffText)
- [ ] BeautifulUI style: ThinkingState, tool-run chips (ToolRunRow/ToolChipRow) — ToolViews.swift, Views/BeautifulUIBridge.swift, Packages/BeautifulUI
- [ ] Wake (background task) chip + WakeDrawer; recap strip + RecapDrawer (folded multiples) — MessageViews.swift (WakeChip, RecapCard, RecapStrip, RecapDrawer)
- [ ] Background task notices with outcome — Views/Chat/TaskNoteRow.swift
- [ ] API error card (human explanation, chips, details copyable, folded repeats, 重试) — Views/Chat/APIErrorCard.swift
- [ ] Claude suggestion pill (填入 / 直接发送) — ChatView.swift (SuggestionPill)
- [ ] Inverted list, follow-end logic, frozen inserts while reading history, ↓ jump button, landing position (landUnderInput / saved position), turn-based pagination (turns=2) with auto-load at top — ChatView.swift (ChatStreamFollow), Shared/ChatStore.swift
- [ ] Local chat cache (last 2 turns per chat, 6 MB) + chats-v2 disk cache — Shared/StateCache.swift, Shared/ChatStore.swift
- [ ] Lazy images on scroll with memory + disk cache — Views/Chat/ChatImageLoader.swift (ChatThumbnail)
- [ ] Collapsing self-drawn header (ChromeHeader: title, subtitle state·machine·space›tab, sibling row, loop/background chips), follows finger, auto-collapse after 3s idle — ChatView.swift
- [ ] Session info sheet (run / usage / location, copy id, copy resume command) — ChatView.swift (SessionInfoSheet)
- [ ] ⋯ menu: 终端, 文件, Git (N), 空间里的所有 Agent, 重命名…, 屏蔽, 拷贝 session id, 拷贝恢复命令, 清空上下文 /clear, 关闭 Agent… — ChatView.swift
- [ ] Swipe left chat → Files → Git; swipe-back gesture re-enabled — Views/Session/SessionPanes.swift (SessionPane, SessionPaneBar), Views/Chat/SwipeBack.swift
- [ ] Hide tab bar in single-column chat — Views/Chat/TabBarHiding.swift
- [ ] Side-vertical toolbar (iPhone Duo) layout — ChatView.swift (sideBarItems, sideHeader)
- [ ] Live status strip (spinner line, output preview, → terminal) — SessionPanes.swift (ActivityChips, ActivitySheet)
- [ ] Loop strip (interval, next wake, task, 停止循环 confirm) + loop tick rows — SessionPanes.swift (LoopStrip, LoopTickRow)
- [ ] Background tasks strip (N 个后台任务, open output file) — SessionPanes.swift (BackgroundTasksStrip)
- [ ] Subagents strip + read-only subagent view + footer 回到主 Agent — SessionPanes.swift (AgentStrip, SubagentFooter)
- [ ] Queue strip (撤回编辑 / 拷贝 / 删除, pull all back) — ChatView.swift (QueueStrip), AppModel.editQueued
- [ ] Skeleton / starting note (「正在启动…」, 「没有启动起来」) — ChatView.swift (ChatSkeleton, StartingNote)
- [ ] Long-press message action bar: 复制 / 选择 / 引用 / 分享 / 编辑 (own); select-text sheet — Views/Chat/MessageMenu.swift
- [ ] Share card image (light/dark, save to Photos, share image/text) — Views/Chat/ShareCard.swift
- [ ] Image viewer (all chat images, paging, zoom, save/edit/share/copy) — Views/Chat/ImageViewer.swift
- [ ] Image editor (box / pen, 6 colors, undo/clear, 加到输入框) — Views/Chat/ImageEditor.swift
- [ ] File path chips (verified via /stat), tool-row paths clickable — Views/Chat/FileViewer.swift (FileChips, LookUpFiles), Code/FilePresence.swift
- [ ] Context reference cards in bubbles (`<jarvis-context>`) — Views/Link/LinkSheet.swift (ContextCards, ContextChips)
- [ ] Send-safety: refusal when computer input has draft → show / clear-and-send — App/SendSafety.swift
- [ ] Frame stats / perf probes (debug) — Views/Chat/FrameStats.swift, App/PerfSignpost.swift

## 5. Composer
- [ ] ComposerRow: + menu (照片 ≤4 / 拍照 / 文件 / 快捷指令), glass input, send / red interrupt — Views/Chat/ComposerParts.swift, ChatView.swift (Composer, CameraPicker)
- [ ] Pending images strip (tap to edit), 1600px compression — ComposerParts.swift (ComposerImages)
- [ ] File upload to the session machine's temp dir, paths inserted at cursor — Views/Chat/FileAttach.swift (UploadsBar)
- [ ] BeautifulUI PromptComposer: @ mentions (session files / other sessions as context), / slash commands (SlashCommand.all, /clear confirm), model (/model alias), dictation — Views/Chat/PromptComposer.swift
- [ ] Dictation (SFSpeechRecognizer zh + AVAudioEngine) — PromptComposer.swift (Dictation)
- [ ] Permission mode + effort chips (SessionModeBar), new-session picker — Views/Chat/SessionModes.swift
- [ ] Drafts persisted per session — AppModel.drafts
- [ ] Context chips above input (from web selection / doc / other session) — LinkSheet.swift (ContextChips)
- [ ] Optimistic send with pending bubble, client_msg_id, retry/discard — Shared/ChatStore.swift, AppModel.send/retry/discard

## 6. Interactions (questions / plans / tool approvals / terminal dialogs)
- [ ] Interaction panel above composer — Views/Interactions/InteractionViews.swift (InteractionPanel)
- [ ] QuestionForm: numbered options, 推荐 tag, 其他回答…, multi-question paging, auto-submit — InteractionViews.swift (QuestionForm, RecommendedTag)
- [ ] PlanForm: markdown preview + fullscreen, 批准并自动执行 / 批准并自动接受编辑 / 打回 with comment — InteractionViews.swift (PlanForm)
- [ ] PermissionForm: 允许 / 拒绝 / 本会话总是允许 — InteractionViews.swift (PermissionForm)
- [ ] ScreenForm 「终端在问你」: parsed dialog → keys, 其他回答 preKeys, 改在对话里说, 查看终端 — InteractionViews.swift (ScreenForm, ScreenSteps, TerminalText)
- [ ] Preview cards in lists with quick buttons — InteractionViews.swift (InteractionPreviewCard, FlowButtons)
- [ ] Phone-tool write confirmation card — AppModel.pendingConfirmation, PhoneTools/PhoneToolRegistry.swift
- [ ] Resolve API — AppModel.resolve

## 7. Terminal
- [ ] Terminal cover (bottom-up full page, drag to dismiss, size/handle/settings bar) — Views/Chat/ChatView.swift (TerminalCover), Views/Session/SessionPanes.swift (TerminalPanel)
- [ ] Live stream (terminal_stream, epoch/seq) or 1s ANSI polling fallback — Views/Session/Terminal/TerminalFeed.swift, SessionPanes.swift (TerminalFeedLink)
- [ ] Grid model + cell renderer (wide chars, box drawing as shapes) — Terminal/TerminalGrid.swift, Terminal/TerminalView.swift (TerminalCanvas), Terminal/BoxDrawing.swift
- [ ] Phone-size takeover (terminal_control), 接管 / 只看 prompt — SessionPanes.swift, AppModel.terminalSubscribe
- [ ] Wrap toggle, themes (Jarvis/Solarized/深夜/ink), font size, pinch zoom — Terminal/TerminalWrap.swift, Terminal/TerminalTheme.swift, TerminalView.swift
- [ ] Selection (long-press word, drag, 拷贝 / 全选), OSC 8 / URL tap → in-app link — TerminalView.swift (SelectionHandle)
- [ ] Scrollback loading 200 lines at a time (screen/before), 回到最新, ~1000-line limit notice — SessionPanes.swift
- [ ] Link status bar (last update, retry backoff, 重试) — SessionPanes.swift (TerminalLinkStatus)
- [ ] Key bar: sticky/locked/held ctrl/alt/shift, esc, tab, ⌃C, D-pad, paste, shortcuts — Views/Chat/TerminalKeys.swift (TerminalKeyBar, KeyCap, DirectionPad)
- [ ] Input: field mode + direct typing mode (UIKeyInput / UITextInput, hardware arrow keys) — Terminal/TerminalInput.swift
- [ ] Gestures: 2-finger swipe sibling agent / adjacent space, double-tap paste (multi-line confirm), scroll in-terminal — TerminalView.swift
- [ ] Shortcuts panel (single tap fill, double tap send), terminal settings (keys shown, auto-hide bar, theme, font, wrap), tips, completions from pane contents — Terminal/TerminalExtras.swift (ShortcutsPanel, TerminalSettingsView, TerminalPreview, ThemeRow, SuggestionStrip), SessionPanes.swift (TerminalTips)

## 8. Files / Git / code viewer
- [ ] Files pane: breadcrumbs, dirs first, size/mtime, git markers, hidden toggle, 拷贝路径, 2000 limit — Views/Session/FilesPane.swift
- [ ] Git pane: branch, upstream ↑↓, stashes, changes (staged/unstaged switch), diff sheet, commit history paging, commit sheet, copy diff/path — Views/Session/GitPane.swift
- [ ] FileViewer: code (syntax, line numbers, copy line/all), JSON outline (fold, copy value/path, expand/collapse all), Markdown rendered/source, images, PDF, directories, share — Views/Chat/FileViewer.swift
- [ ] Paged file fetch (2 MB pages) — Code/CodeFileSource.swift
- [ ] Line splitting, background highlighting (2000-line chunks, caps) — Code/CodeDocument.swift
- [ ] Virtualized code lines view (UILabel reuse, context menu) — Code/CodeLinesView.swift
- [ ] JSON outline — Code/JSONOutline.swift
- [ ] Markdown chunking — Code/MarkdownChunks.swift
- [ ] Syntax languages + lexer — Code/SyntaxLanguage.swift, Code/SyntaxLexer.swift
- [ ] Monospace scrollable text (diffs, output) — Views/Chat/MonoTextView.swift
- [ ] Path presence cache — Code/FilePresence.swift; perf log — Code/FilePerf.swift

## 9. Link viewer (in-app web)
- [ ] Bottom sheet opening full height, 60% detent with background interaction — Views/Link/LinkSheet.swift (LinkSheet)
- [ ] WKWebView pool (preload first 3 links per reply, LRU 6, shared data store, cache policy) — LinkSheet.swift (WebViewPool, PooledWebView)
- [ ] Text selection menu: 「AI 修改这段」, 「作为上下文」; 选择文字… sheet; 在 Safari 打开; share — LinkSheet.swift (SelectableWebView, PickTextSheet, DocPickMenu)
- [ ] Feishu docs/wiki links → native reader — Views/Feishu/Docs/FeishuDocReader.swift

## 10. New chat form
- [ ] Machine picker, launcher picker (per-machine launchers, 跟随启动器), directory browser with search, 加入空间, title, first prompt, mode picker; remembers machine/launcher/cwd for Siri — Views/NewChat/NewChatView.swift (NewChatView, LauncherPicker, DirectoryBrowser)
- [ ] Prefilled new chat (from mail 「让 Claude 处理」) — Views/Mail/MailViews.swift (NewChatPrefilled)

## 11. Machines / monitoring
- [ ] 设置 › 机器 list: online, sessions, version, launcher, reconnect; version-mismatch 重新部署 — Views/Machines/MachinesView.swift (MachinesView, MachineCard, DeployRow)
- [ ] Machine detail (系统与监控, running Claude list) — MachinesView.swift (MachineDetailView)
- [ ] Dashboard: N/M online, per-machine card with CPU/mem/net now + 1h sparklines, alert rings, 10s polling — Views/Machines/MachineDashboardView.swift
- [ ] Monitor page: 1/6/24h, CPU/mem/net/load charts with shared scrub, per-core bars, system facts, gaps — Views/Machines/MachineMonitorView.swift, Views/Machines/MetricCharts.swift (Swift Charts)
- [ ] OS badge — Views/Machines/OSBadge.swift; model — Shared/MachineMetrics.swift

## 12. Knowledge base
- [ ] Kind pills (项目/会话/人/文档/会议/日程 with counts), tag filter, project sections (重点/其他进行中/已完成/不是项目) — Views/Knowledge/KnowledgeView.swift (KnowledgeView, KBKindBar, KBRow, KBAvatar, ImportanceBadge, KBPill)
- [ ] Full-text search, merged per entry, highlighted hits — KnowledgeView.swift (KBHitRow)
- [ ] Detail: header, 问 Jarvis / 开 Agent, agents, timeline, profile, relations, code dirs, last 7 days messages, path; search matches — KnowledgeView.swift (KnowledgeDetailView, KBMatchRow, KBTimelineRow, KBLinkRow, KBMessageText)
- [ ] Day view of chat messages with prev/next — KnowledgeView.swift (KBDayView)
- [ ] 打标 editor (importance, relevance, tags, project type/status) — KnowledgeView.swift (KBTagEditor)
- [ ] Models — Shared/Knowledge.swift

## 13. Inbox (收件箱)
- [ ] Unified list (Feishu + mail + docs + waiting agents), filters (全部/飞书/each mailbox | @我/私聊/群聊/邮件/Agent), search, sections 需要你 / 知悉即可 (全部看过) / 已折叠 (全部清除) / 已处理, swipe = done, pull refresh — Views/Inbox/InboxView.swift (InboxView, InboxRow, InboxAgentRow)
- [ ] AI draft card (发送 / 修改), 「AI 分身正在处理」 — InboxView.swift (InboxDraftCard)
- [ ] Detail: Lark chat at first new message or mail; 已处理; action bar (摘要, AI 草拟回复, 交给 AI) — InboxView.swift (InboxDetailView), Views/Inbox/InboxActions.swift (InboxActionBar)
- [ ] Draft sheet (edit, 让 AI 改, 重写, doc status changes) — InboxActions.swift (InboxDraftSheet)
- [ ] Hand-off sheet (Jarvis new chat / one of 8 agents, presets, mark done) — InboxActions.swift (InboxHandOffSheet)
- [ ] Cloud doc items: doc icon, rows mentioning me, status menu writes to sheet/base, detail page — Views/Inbox/InboxDocViews.swift
- [ ] Store: cache, optimistic done, badge, unsent replies — App/InboxStore.swift; models Shared/InboxModels.swift

## 14. Feishu views
- [ ] Lark chat: avatars, sender names, 5-min grouping, date dividers, reply quote, reactions (toggle), edited, new-messages divider, load earlier, topic groups + thread view, focus message, mark read only seen — Views/Inbox/Lark/LarkChatView.swift
- [ ] Send with outbox (sending, failed → resend/edit/delete) — Views/Inbox/Lark/LarkOutbox.swift
- [ ] Paging — Views/Inbox/Lark/LarkPager.swift; realtime sync + background cache — App/LarkSync.swift
- [ ] Rich content: rich-text blocks, images, stickers, file cards, video, voice, code, system msgs — Views/Inbox/Lark/LarkMessageContent.swift
- [ ] Inline runs + emoji images — Views/Inbox/Lark/LarkInline.swift
- [ ] Interactive cards (header, markdown, columns, table, list, panel, note, tag, icon, buttons with callback, overflow, chart) — Views/Inbox/Lark/LarkCard.swift
- [ ] Summary cards (merge forward, share chat/user, meeting, calendar, vote) — Views/Inbox/Lark/LarkSummaryCards.swift
- [ ] Media: authenticated image cache, gallery, video player, Quick Look files — Views/Inbox/Lark/LarkMedia.swift
- [ ] Profile card (发消息) — Views/Inbox/Lark/LarkPeople.swift
- [ ] Reaction picker, removed (Moments) card + post view — Views/Inbox/Lark/LarkReactionsMoments.swift
- [ ] Translation line under messages — LarkChatView.swift (LarkTranslationLine)
- [ ] 「让 Jarvis 总结」 action — LarkChatView.swift
- [ ] Time / name tag — Views/Inbox/Lark/LarkTime.swift; models — Views/Inbox/Lark/LarkModels.swift
- [ ] Feishu doc reader: blocks, tables, images, files, link cards, outline, comments (resolved/unresolved), AI 摘要, 作为上下文, 在飞书中打开, share — Views/Feishu/Docs/*.swift
- [ ] Feishu raw API routing + app icon — Views/Feishu/Foundation/FeishuFoundation.swift

## 15. Mail
- [ ] Mail detail (HTML via WKWebView, attachments, mark seen, translation banner/显示原文, reply, 让 Claude 处理) — Views/Mail/MailViews.swift (MailDetailView, HTMLView)
- [ ] Compose / reply (account, to, cc, subject, body) — MailViews.swift (ComposeView)
- [ ] Accounts management (list, status, provider marks) — MailViews.swift (MailAccountsView, MailProviderMark)
- [ ] Add account: manual IMAP/SMTP form, 「AI 一键添加」 paste recognition (clipboard), Sign in with Google (web auth session) — MailViews.swift (AddMailAccountView, GoogleSignInLabel), Views/Mail/MailAccountForm.swift

## 16. Model launchers
- [ ] Launcher list (Jarvis-managed + ~/.zshrc, sources, hidden) with search — Views/Settings/ModelLaunchersView.swift (ModelLaunchersView, LauncherRow)
- [ ] Detail: per-machine params, function source (masked), 测试可用性, delete/hide with popover confirm, restore — ModelLaunchersView.swift (ModelLauncherDetailView, LauncherDetailSections, LauncherCheckRow)
- [ ] Editor (name, base URL, API key, model, small model, args) — ModelLaunchersView.swift (LauncherEditor)
- [ ] Quota section (pools/windows, colors ≥70/90%, refresh) — Views/Settings/LauncherQuotaViews.swift
- [ ] Config models — Shared/LauncherConfig.swift

## 17. Personas & styles
- [ ] Persona picker / gallery (follow style colors), persona store (download, cache by rev, APNG frames) — Views/Settings/PersonaSettingsView.swift, Persona/PersonaStore.swift, Persona/Persona.swift, Persona/PersonaViews.swift (PersonaAvatar, PersonaDot, PersonaFramesView)
- [ ] Persona dot states idle/thinking/working/needs_you/offline, reduce-motion = frame 0 — PersonaViews.swift, AssistantTopBar.swift
- [ ] UI styles 液态玻璃 / Claude / 黏土 / Island / 飞书 / BeautifulUI with preview cards — Views/Settings/SettingsView.swift (StylePicker, StyleCard), Shared/Design/StyleSpec.swift, Shared/Design/Styles/*.swift
- [ ] Text size 小/标准/大 over Dynamic Type — DesignSystem/TextSize.swift
- [ ] Theme tokens, icons, palettes, status palette — Shared/Design/Theme.swift, Icons.swift, Palette.swift, PersonaPalette.swift, Shared/StatusPalette.swift
- [ ] Components (StatusDot, Chip, CountBadge, EmptyStateView, Monogram…) and surfaces/buttons — DesignSystem/Components.swift, DesignSystem/Styling.swift

## 18. Notifications & notification actions
- [ ] Request permission + APNs registration, POST /devices — App/JarvisApp.swift (AppDelegate.registerIfPossible)
- [ ] Categories INTERACTION_PERMISSION (允许 auth-required / 拒绝) and INTERACTION_PLAN (批准 / 打回修改 text input) — App/NotificationActions.swift
- [ ] Background resolve from action, failure local notification — App/JarvisApp.swift (resolveFromNotification)
- [ ] Tap opens session (main-queue completion handler), suppress while viewing — JarvisApp.swift
- [ ] Silent push handling (wake / disconnect) — JarvisApp.swift
- [ ] Settings: 需要你处理时, Agent 做完时 (不推送/只推长任务/每次都推), per-device prefs on hub, system-disabled warning + open settings — Views/Settings/NotificationSettingsView.swift

## 19. Live Activity
- [ ] Fleet activity (needsYou/working/done), push-to-start + push token upload, start/update/end (1 min grace), stale handling — App/LiveActivityController.swift, LiveActivity/JarvisActivityAttributes.swift, Shared/FleetState.swift
- [ ] Lock screen + Dynamic Island views, watch smart stack — JarvisWidgets/FleetLiveActivity.swift, LiveActivity/FleetIslandViews.swift
- [ ] Buttons: Allow/Deny permission, Approve plan (LiveActivityIntent run in app) — LiveActivity/FleetIntents.swift, App/FleetActions.swift
- [ ] Island settings (presets A/B/C/custom, load machine, previews), lock details toggle — Views/Settings/IslandSettingsView.swift, NotificationSettingsView.swift

## 20. Siri / App Intents
- [ ] 问 Claude (machine param, new session, returns OpenSession) — Intents/AskClaudeIntent.swift
- [ ] Jarvis 状态 — Intents/JarvisStatusIntent.swift
- [ ] 批准第一个待处理 — Intents/ApproveFirstIntent.swift
- [ ] 打开会话 (foreground) + SessionEntity — Intents/OpenSessionIntent.swift
- [ ] MachineEntity query — Intents/MachineEntity.swift
- [ ] Shortcuts phrases — Intents/JarvisShortcuts.swift; texts / picker / errors — Intents/IntentSupport.swift

## 21. Phone tools (phone_* MCP)
- [ ] Register enabled tools on connect, tool_call → tool_result, write confirmations — PhoneTools/PhoneToolRegistry.swift, PhoneTool.swift, PhoneToolSupport.swift, AppModel.registerPhone
- [ ] Contacts search — PhoneTools/ContactsTool.swift
- [ ] Calendar events list/create — PhoneTools/CalendarTool.swift
- [ ] Reminders list/create/complete — PhoneTools/RemindersTool.swift
- [ ] Current location — PhoneTools/LocationTool.swift
- [ ] Health summary — PhoneTools/HealthTool.swift
- [ ] Photos recent / get — PhoneTools/PhotosTool.swift
- [ ] Device info — PhoneTools/DeviceTool.swift
- [ ] Clipboard get/set — PhoneTools/ClipboardTool.swift
- [ ] Local notify — PhoneTools/NotificationsTool.swift
- [ ] Authorization status / request — PhoneTools/PhoneAccess.swift
- [ ] Settings › 手机数据 per-source toggles + 授权 — Views/Settings/SettingsView.swift (PhoneDataView)

## 22. Demo mode
- [ ] 「没有 hub？先体验一下」, ServerConfig.demo, local APIClient + local socket, Demo.json with relative times, generated metrics, echo replies, demo errors shown as blue info — App/DemoHub.swift, App/AppModel.swift (enterDemo), Shared/APIClient.swift, Shared/LiveConnection.swift
- [ ] Settings demo section (连接我的 hub / 退出体验模式), 体验 tab badge — SettingsView.swift (DemoSection), RootView.swift

## 23. Pairing / credentials / watch
- [ ] Welcome page, QR scan (VisionKit), manual entry, jarvis://pair link — Views/Settings/SettingsView.swift (WelcomeView, PairView, QRScanner)
- [ ] Keychain storage — Shared/Keychain.swift
- [ ] Device token exchange, 设置 › 设备 list + revoke — App/DeviceCredentials.swift (DevicesView)
- [ ] 重新配对, 断开并忘记服务器 — SettingsView.swift
- [ ] Sync to Apple Watch (WCSession) — Shared/WatchSync.swift, SettingsView.swift; watch app — JarvisWatch/JarvisWatchApp.swift (macOS: n/a)

## 24. Offline / outbox / connection
- [ ] Connection status waiting/connecting/updating (NWPathMonitor, ping 5s, debounce 1.5s), title replacement, details popover (立即重试) — App/ConnectionStatus.swift, Views/ConnectionTitle.swift
- [ ] WebSocket with backoff + since_seq resubscribe — Shared/LiveConnection.swift
- [ ] Outbox: offline messages persisted, auto-send in order after catch-up — App/Outbox.swift
- [ ] One-shot actions not queued; blue 「刚才的操作没有生效」 — AppModel.show / resolve
- [ ] Background delta sync for agents (rev/after_seq, 40 sessions, concurrency 3) — App/AgentSync.swift
- [ ] State cache for machines/sessions/interactions/inbox/mail accounts/lark chats — Shared/StateCache.swift

## 25. Search
- [ ] Agents search — ChatsView.swift (searchable, SearchCloseProbe)
- [ ] Assistant sidebar search — AssistantSidebar.swift
- [ ] Inbox search — InboxView.swift
- [ ] Knowledge search — KnowledgeView.swift
- [ ] Launcher search, directory search, translation chat picker search — ModelLaunchersView.swift, NewChatView.swift, TranslationSettingsView.swift

## 26. Settings pages
- [ ] Root: header (demo note), 形象, 界面风格 + 字号, 助手, 模型, Claude Code 设置, 机器, 设备, 手机数据, 通知, 翻译, 存储空间, 服务器, 关于, 同步到 Apple Watch — Views/Settings/SettingsView.swift (SettingsDestinations, SettingsPage)
- [ ] 助手 → profile page; 定时任务 (upcoming timeline, daily/interval/once rows, toggle, swipe delete, long-press run now, templates, editor with model/permission/effort, bypass confirm, continue conversation, history) — Views/Settings/AssistantProactiveViews.swift (AssistantRoutinesView, AssistantTaskRow, AssistantTaskEditor, AssistantWeekdayPicker), Views/Settings/AssistantRoutinesModel.swift
- [ ] 记忆 (frontmatter, type tags, markdown detail, edit, swipe delete, MEMORY.md read-only) — AssistantProactiveViews.swift (AssistantMemoryView, AssistantMemoryDetail, AssistantMemoryEditor)
- [ ] 关于我 notes — AssistantProactiveViews.swift (AssistantNotesView)
- [ ] Claude Code 设置 (settings.json whitelist keys, apply to all machines) — SettingsView.swift (ClaudeSettingsView)
- [ ] 翻译 (auto translate, mail, chat picker) + Translator (NLLanguageRecognizer, batching) — Views/Settings/TranslationSettingsView.swift, App/Translator.swift
- [ ] 存储空间 (per category sizes, clear one / all, keep web logins) — Views/Settings/StorageView.swift
- [ ] 通知 / 灵动岛 — see §18, §19



## UIKit dependencies (`Jarvis/App` and `Shared`)

| File | What it uses |
| --- | --- |
| `Jarvis/App/AppModel.swift` | `UIImpactFeedbackGenerator`, `UINotificationFeedbackGenerator` (haptics), `UIImage` (`composerInbox`), `UIDevice.current` |
| `Jarvis/App/JarvisApp.swift` | `UIApplicationDelegate` / `UIApplicationDelegateAdaptor`, `UIApplication.shared` (`registerForRemoteNotifications`, `applicationState`, background task id), `UIBackgroundFetchResult` (silent push), `didEnterBackgroundNotification`, `UIDevice.current` |
| `Jarvis/App/LiveActivityController.swift` | `UIApplication.shared` (foreground state) |
| `Jarvis/App/PerfSignpost.swift` | `UIView`, `UIViewRepresentable`, `UITraitCollection` (debug probe) |
| `Jarvis/App/DeviceCredentials.swift` | `UIDevice.current` (device name) |
| `Shared/StatusPalette.swift` | `UIColor` dynamic provider + `UIColor(hex:)` extension (`#if !os(watchOS)`) |
| `Shared/Design/Theme.swift` | `Color(uiColor:)` with `.systemBackground`, `.secondarySystemBackground`, `.quaternary/tertiary/secondarySystemFill` |
| `Shared/WatchSync.swift` | WatchConnectivity, not UIKit (`#if os(iOS)` / `os(watchOS)` branches) |
| `Shared/Keychain.swift` | Security only; already portable |

Notable UIKit outside those two folders (the macOS work will hit these too):
- **Terminal:** `Session/Terminal/TerminalView.swift` is a full UIKit view: `UIScrollView`, gestures, `UIEditMenuInteraction`, `UIFont`, `UIGraphicsGetCurrentContext`. `TerminalInput.swift` uses `UIKeyInput`/`UITextInput` and `UIKeyCommand` for arrows and esc. `Chat/TerminalKeys.swift` handles raw `UITouch`.
- **Assistant drawer:** `Assistant/AssistantSidebar.swift` uses `UIPanGestureRecognizer`, `UIHostingController`, `UITabBar` and `UIView.animate`.
- **Code and text views:** `Code/CodeLinesView.swift` (reused `UILabel`s, `UIContextMenuInteraction`), `Chat/MonoTextView.swift` (`UITextView` inside `UIScrollView`), `Chat/MessageMenu.swift` (long-press and swipe recognizers, `UITextView`), `Chat/ImageViewer.swift` (zooming `UIScrollView`).
- **Camera and drawing:** `Chat/ChatView.swift` uses `UIImagePickerController` for the camera. `Chat/ImageEditor.swift` and `PhoneTools/PhotosTool.swift` use `UIGraphicsImageRenderer`.
- **Navigation chrome:** `Chats/ChatsView.swift` (`UISearchController` probe), `Chat/SwipeBack.swift` (`UINavigationController` pop gesture), `Chat/TabBarHiding.swift`.
- **Link sheet:** `Link/LinkSheet.swift` uses `UITextView`, `UIMenuBuilder` and `UIApplication.open`.
- **Everywhere else:**
  - `UIPasteboard.general` appears in about 20 view files.
  - `UIImage` in the media, persona and loader files.
  - `UIColor` in `GitPane`, `TerminalTheme`, `FeishuDocRenderer` and `LarkInline`.
  - `UIFontMetrics` and `UIContentSizeCategory` in `TextSize`.
  - `UIDevice` in `SplitNavigation` and `DeviceTool`.
  - The open-settings URLs in `NotificationSettingsView` and `SettingsView`.
  - `UIImageView` in `PersonaViews`.

There are also many iOS-only SwiftUI modifiers: `navigationBarTitleDisplayMode`, `fullScreenCover`, `topBarLeading`/`topBarTrailing`, `textInputAutocapitalization`, `keyboardType`, `presentationDetents`, `presentationBackgroundInteraction`, `statusBarHidden`, and `toolbarVisibility(.tabBar)`.

## iOS-only frameworks and where they're used

| Framework | Used in |
| --- | --- |
| HealthKit | `PhoneTools/HealthTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| ActivityKit | `App/LiveActivityController.swift`, `Views/Settings/NotificationSettingsView.swift`, `LiveActivity/JarvisActivityAttributes.swift`, `JarvisWidgets/FleetLiveActivity.swift` (WidgetKit too) |
| WatchConnectivity | `Shared/WatchSync.swift`, `App/DeviceCredentials.swift` |
| PhotosUI (`PhotosPicker`) | `Views/Chat/ChatView.swift`, `Views/Assistant/AssistantView.swift` |
| Photos | `PhoneTools/PhotosTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift`, `Views/Chat/ImageViewer.swift` (save), `Views/Chat/ShareCard.swift` (save) |
| AVFoundation | `Views/Chat/PromptComposer.swift` (`AVAudioEngine`/`AVAudioSession` for dictation), `PhoneTools/PhoneToolRegistry.swift`. The camera is `UIImagePickerController`, not AVFoundation. |
| QR scanning | VisionKit `DataScannerViewController` in `Views/Settings/SettingsView.swift`, not AVFoundation |
| Speech | `Views/Chat/PromptComposer.swift` |
| CoreLocation | `PhoneTools/LocationTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| Contacts | `PhoneTools/ContactsTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| EventKit | `PhoneTools/CalendarTool.swift`, `RemindersTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| AppIntents | `Jarvis/Intents/*.swift` (7 files), `LiveActivity/FleetIntents.swift` |
| UserNotifications | `App/AppModel.swift`, `App/FleetActions.swift`, `App/JarvisApp.swift`, `App/NotificationActions.swift`, `PhoneTools/NotificationsTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift`, `Views/Settings/NotificationSettingsView.swift` |
| WebKit | `Views/Link/LinkSheet.swift`, `Views/Mail/MailViews.swift`, `Views/Settings/StorageView.swift` |
| Other | QuickLook and AVKit in `Views/Inbox/Lark/LarkMedia.swift`; PDFKit in `Views/Chat/FileViewer.swift`; Charts in `Lark/LarkCard.swift` and `Machines/MetricCharts.swift`; NaturalLanguage in `App/Translator.swift`; Network (`NWPathMonitor`) in `App/ConnectionStatus.swift` and `PhoneTools/DeviceTool.swift`; ImageIO in `Persona/PersonaStore.swift` and `LarkMedia.swift`; AuthenticationServices via the SwiftUI `webAuthenticationSession` for Google sign-in in `Views/Mail/MailViews.swift`; UniformTypeIdentifiers in `Chat/FileAttach.swift` |

Most of these also exist on macOS. The ones that don't have a direct Mac equivalent:
- ActivityKit and Live Activities.
- WatchConnectivity.
- HealthKit as a data source. It's iOS-first, so check what macOS actually supports before relying on it.
- `UIImagePickerController` and the VisionKit data scanner.
- `UIKeyInput`/`UITextInput` terminal input (needs an `NSTextInputClient` port).
- Notification `.authenticationRequired` actions and other lock-screen-specific behavior.