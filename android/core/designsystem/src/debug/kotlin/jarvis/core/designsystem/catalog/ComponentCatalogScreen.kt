package jarvis.core.designsystem.catalog

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons

/** Debug-only gallery of the design system in the active style. Every sample is local; nothing is sent. */
@Composable
fun ComponentCatalogScreen(modifier: Modifier = Modifier, onBack: (() -> Unit)? = null) {
    var segment by remember { mutableIntStateOf(0) }
    var bay by remember { mutableIntStateOf(1) }
    var toggled by remember { mutableStateOf(true) }
    var text by remember { mutableStateOf("") }
    var expanded by remember { mutableStateOf(true) }
    var ready by remember { mutableStateOf(false) }
    var feedback by remember { mutableStateOf("示例操作只在本页展示，不会发出请求。") }
    var chip by remember { mutableStateOf("") }
    val palette = JarvisTheme.palette
    JListPage(
        title = "组件目录", subtitle = "当前风格：${JarvisTheme.style.label}", onBack = onBack,
        modifier = modifier.testTag("component-catalog"),
    ) {
        item { Text(feedback, style = JarvisTheme.type.caption, color = palette.ink3) }
        // Controls that turn enabled after being disabled must keep their fill and frame (Modifier.stableAlpha).
        section("填好才能点") {
            JSwitchRow("示例已填好", ready, { ready = it })
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                JButton("下一步", { feedback = "下一步" }, Modifier.weight(1f), enabled = ready)
                JButton("保存", { feedback = "保存" }, Modifier.weight(1f), kind = JButtonKind.Secondary, enabled = ready)
            }
            JTextField("hub.example.com", {}, label = "hub 地址", enabled = ready, singleLine = true)
            DecisionOption("允许这一次", { feedback = "允许" }, recommended = true, enabled = ready)
        }
        section("按钮") {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                JButton("放行", { feedback = "主按钮" }, Modifier.weight(1f))
                JButton("拒绝", { feedback = "次按钮" }, Modifier.weight(1f), kind = JButtonKind.Secondary)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                JButton("关闭 Agent", { feedback = "危险操作" }, kind = JButtonKind.Destructive, size = JButtonSize.Medium)
                JButton("发送", {}, size = JButtonSize.Small, icon = PhIcons.PaperPlaneTilt)
                JTextButton("查看全部", { feedback = "文字按钮" })
            }
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                JButton("正在处理", {}, Modifier.weight(1f), loading = true, loadingText = "正在发送…")
                JButton("不可用", {}, Modifier.weight(1f), kind = JButtonKind.Secondary, enabled = false)
            }
            Row { listOf(PhIcons.MagnifyingGlass, PhIcons.Plus, PhIcons.SlidersHorizontal).forEach { JIconButton(it, null, {}, raised = true) } }
        }
        section("输入与选择") {
            JSearchField(text, { text = it }, "搜索空间、会话或最新消息")
            JTextField("", {}, label = "拒绝原因（可选）", placeholder = "写给 Agent 的一句话")
            JSegmented(listOf(JSegment("需要你", 3), JSegment("全部", 11), JSegment("已处理")), segment, { segment = it })
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                JChip("全部机器", icon = PhIcons.Laptop, onClick = {})
                JChip("自动改文件", selected = true, onClick = {})
                JTag("草稿", status = Status.NeedsYou)
                JBadge(3)
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                JSwitch(toggled, { toggled = it }); JCheckbox(toggled, { toggled = it }); JRadio(toggled, { toggled = !toggled })
            }
        }
        item { JSectionHeader("快速筛选") }
        item {
            QuickFilterBar(
                listOf(Bay("全部", 9), Bay("需要你", 3, Status.NeedsYou), Bay("工作中", 2, Status.Working), Bay("未读", 1, Status.Unread)),
                bay, { bay = it },
            )
        }
        item { JSectionHeader("等你定", count = 2) }
        item {
            DecisionCard(
                DecisionKind.Plan, "照片同步脚本支持 HEIC", source = "Mac Studio / photo-sync", expires = "23 分钟后过期",
                summary = "用 sips 把 HEIC 转成 JPEG", advice = "只动导出流程，原文件不受影响", onOpen = { feedback = "打开会话" },
                body = { Text("1  用 sips 把 HEIC 转成 JPEG，质量 90\n2  转换放在导出之前，原文件不动", style = JarvisTheme.type.callout, color = palette.ink2) },
                actions = { DecisionButtons(JAction("批准") { feedback = "批准" }, secondary = JAction("先不") { feedback = "先不" }) },
            )
        }
        item { Spacer(Modifier.height(12.dp)) }
        item {
            DecisionCard(
                DecisionKind.Question, "补齐网络层的单元测试", source = "MacBook / weather", expires = "27 分钟后过期",
                expandedByDefault = false, summary = "网络层的测试用哪个框架？",
                quickActions = { JButton("Swift Testing", {}, size = JButtonSize.Small); JButton("XCTest", {}, kind = JButtonKind.Secondary, size = JButtonSize.Small) },
                body = {
                    Text("网络层的测试用哪个框架？", style = JarvisTheme.type.body, color = palette.ink)
                    Spacer(Modifier.height(10.dp))
                    DecisionOption("Swift Testing", { feedback = "选了推荐项" }, detail = "新代码统一用 @Test", recommended = true)
                    Spacer(Modifier.height(8.dp))
                    DecisionOption("XCTest", { feedback = "选了另一项" }, detail = "和现有的 UI 测试保持一致")
                },
            )
        }
        // The Agents list, line by line as on the Agents tab: computer chips, then 电脑 → 空间 → 会话.
        item { JSectionHeader("Agents") }
        item("agents-chips") {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                MachineChip("全部机器", 7, selected = chip == "", onClick = { chip = "" })
                MachineChip("MacBook", 5, selected = chip == "laptop", onClick = { chip = if (chip == "laptop") "" else "laptop" }, icon = PhIcons.Laptop, online = true)
                MachineChip("服务器", 2, selected = chip == "server", onClick = { chip = if (chip == "server") "" else "server" }, icon = PhIcons.HardDrives, online = false)
            }
        }
        item("agents-machine") {
            AgentTreeHeader(
                "MacBook", folded = false, onToggle = { feedback = "折叠 MacBook" }, first = true, icon = PhIcons.Laptop, online = true,
                detail = "2 个空间，5 个会话", counts = listOf(Status.NeedsYou to 1, Status.Working to 1, Status.Unread to 1),
                onMonitor = { feedback = "机器监控" },
            )
        }
        item("agents-space") {
            AgentSpaceHeader(
                "weather", folded = false, onToggle = { feedback = "折叠 weather" }, first = true,
                counts = listOf(Status.NeedsYou to 1, Status.Working to 1, Status.Unread to 1), flags = listOf(RowFlag("空间已置顶", PhIcons.PushPin)),
                menu = { HeadingAction(PhIcons.DotsThree, "空间操作", onClick = { feedback = "空间操作：打开空间、全部标为已读、置顶、屏蔽、重命名" }) },
            )
        }
        val rows = listOf(
            AgentSample("补齐网络层的单元测试", Status.NeedsYou, "网络层的测试用哪个框架？", "3 分钟前", agent = "Codex", facts = listOf(RowFact("1 件事等你定", caution = true))),
            AgentSample("给天气 App 加主屏幕小组件", Status.Working, "正在跑 xcodebuild test", "刚刚", facts = listOf(RowFact("后台 1"), RowFact("草稿：顺便看下深色模式", caution = true))),
            AgentSample("菜谱网站加搜索", Status.Unread, "可以按食材和用时筛选了", "9 分钟前", flags = listOf(RowFlag("已置顶", PhIcons.PushPin))),
        )
        rows.forEachIndexed { index, row ->
            item("agents-row-$index") {
                AgentRow(
                    row.title, row.status, onClick = { feedback = row.title }, onLongClick = { feedback = "长按：${row.title}" }, depth = 1,
                    position = GroupPosition.of(index, rows.size), preview = row.preview, time = row.time, agent = row.agent,
                    statusText = row.status.label, facts = row.facts, flags = row.flags,
                )
            }
        }
        item("agents-idle") { AgentIdleRow(2, onClick = { feedback = "展开空闲的会话" }) }
        item("agents-quiet") { AgentSpaceHeader("dotfiles", folded = true, onToggle = { feedback = "展开 dotfiles" }, quiet = true, sessions = 3) }
        item("agents-offline") {
            AgentTreeHeader("服务器", folded = true, onToggle = { feedback = "展开服务器" }, icon = PhIcons.HardDrives, online = false, detail = "1 个空间，2 个会话")
        }
        item("agents-pinned") { AgentTreeHeader("置顶", folded = false, onToggle = { feedback = "折叠置顶" }, icon = PhIcons.PushPin, counts = listOf(Status.Working to 1)) }
        item("agents-located") {
            AgentRow(
                "整理发布说明", Status.Working, onClick = { feedback = "整理发布说明" }, position = GroupPosition(first = true, last = false),
                preview = "在对比两个版本的改动", time = "1 分钟前", statusText = Status.Working.label,
                facts = listOf(RowFact("MacBook / blog"), RowFact("循环，下次 10:00")), flags = listOf(RowFlag("已置顶", PhIcons.PushPin)),
            )
        }
        item("agents-compact") {
            AgentRow(
                "清理旧的检查点", Status.Idle, onClick = { feedback = "清理旧的检查点" }, position = GroupPosition(first = false, last = true),
                time = "2 小时前", compact = true, muted = true, flags = listOf(RowFlag("已屏蔽", PhIcons.Prohibit)),
            )
        }
        item { JSectionHeader("对话") }
        item { UserBubble("给天气 App 加一个主屏幕小组件：显示当前温度和接下来 6 小时的降雨概率。", time = "08:01") }
        item {
            WorkGroup("做了 4 步，用时 7 分钟", expanded, { expanded = !expanded }) {
                WorkStep(PhIcons.Lightbulb, "时间线按整点刷新，比每 15 分钟刷新更省电", verb = "思考")
                WorkStep(PhIcons.PencilSimple, "Provider.swift", verb = "编辑", detail = "WeatherWidget", added = 42, removed = 3)
                WorkStep(PhIcons.TerminalWindow, "swift test", verb = "运行", detail = "48 个测试通过", mono = true)
                WorkStep(PhIcons.TerminalWindow, "xcodebuild test", verb = "运行", state = WorkState.Running, mono = true, last = true)
            }
        }
        item { IncomingBubbleFrame(author = "示例联系人4", time = "16:24", avatar = { JAvatar("示", size = 32.dp) }) { Text("大家先看下图标的配色", style = JarvisTheme.type.body, color = palette.ink) } }
        item { TimelineMark("这件事从这里开始", emphasized = true) }
        item { StatusLine(StatusLineState(working = true, workDetail = "第 12 步"), chips = listOf("循环", "后台 1"), onClick = {}) }
        item {
            ComposerFrame(leading = { JIconButton(PhIcons.Plus, "附件", {}) }, trailing = { SendButton({}) }) {
                ComposerTextField(text, { text = it }, "补充说明，这一步做完就发给它")
            }
        }
        item { JSectionHeader("收件箱") }
        item {
            MessageItem(
                "示例联系人6", "周五的发布能赶上吗？", onClick = {}, source = MessageSource.Lark, sourceLabel = "飞书", time = "08:07",
                preview = "下午要和客户确认时间。", status = Status.NeedsYou, unread = 2,
                extra = { DraftPreview("能赶上。剩下两个阻塞问题今天都会合进去。", actions = { JButton("改一改", {}, kind = JButtonKind.Secondary, size = JButtonSize.Small); JButton("发送", {}, size = JButtonSize.Small) }) },
            )
        }
        item { JSectionHeader("设置") }
        item { HubCard("Jarvis hub", connected = true, statusText = "已连接，内网直连", facts = listOf("电脑在线" to "3 台", "证书" to "已钉扎"), onClick = {}) }
        item { Spacer(Modifier.height(12.dp)) }
        item { JGroup(rows = listOf({ JRow("配对", leadingIcon = PhIcons.QrCode, subtitle = "扫码、粘贴链接或手动输入", onClick = {}) }, { JSwitchRow("锁屏实时状态", toggled, { toggled = it }, leadingIcon = PhIcons.Broadcast) })) }
        // A row with several lines (the slot JRow) and a long value under its label (rowPadding) line up with plain rows.
        item { JSectionHeader("多行的行") }
        item { JGroupItem(GroupPosition.of(0, 3)) { JRow("正在使用", subtitle = "https://hub.example.com:18662", value = "自动") } }
        item {
            JGroupItem(GroupPosition.of(1, 3)) {
                JRow(
                    onClick = { feedback = "候选地址的操作" }, onLongClick = { feedback = "候选地址的操作" }, onClickLabel = "更多操作",
                    trailing = { JButton("测试", { feedback = "测试" }, kind = JButtonKind.Secondary, size = JButtonSize.Small, icon = PhIcons.Gauge) },
                ) {
                    JStatusDot(Status.Unread, Modifier.align(Alignment.Top).padding(top = 8.dp))
                    Spacer(Modifier.width(12.dp))
                    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        Text("https://[2001:db8::10]:18662", style = JarvisTheme.type.body, color = palette.ink)
                        Text("hub 下发", style = JarvisTheme.type.caption, color = palette.ink2)
                        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) { JTag("IPv6"); JTag("HTTPS", status = Status.Unread) }
                        Text("上次测试：通过", style = JarvisTheme.type.caption, color = palette.ink3)
                    }
                }
            }
        }
        item {
            JGroupItem(GroupPosition.of(2, 3)) {
                JRow("证书公钥指纹")
                Text("SHA256:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF（示例）", Modifier.padding(horizontal = rowPadding).padding(bottom = 12.dp),
                    style = JarvisTheme.type.micro, color = palette.ink2)
            }
        }
        section("提示与空状态") {
            JNotice("hub 的证书和配对时不一样，不会自动重连。", title = "hub 的证书变了", status = Status.NeedsYou, icon = PhIcons.ShieldWarning, actionLabel = "连接诊断", onAction = {})
            PendingBadge(PendingStatus.Waiting)
            StepProgress(listOf(ProgressStep("连接 hub", StepState.Complete), ProgressStep("核对身份", StepState.Active), ProgressStep("同步会话")))
            JEmptyState("还没有会话", "连接电脑后，就可以在这里开始对话。", icon = PhIcons.Robot, action = { JButton("新建会话", {}) })
        }
        section("状态色") {
            Status.entries.forEach { status ->
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(palette.status(status).tint).padding(12.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) { JStatusMark(status, animated = false); StatusLabel(status) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(palette.background, palette.surfaceOpaque, palette.ink, palette.accent, palette.marker, palette.positive, palette.danger.dot).forEach {
                    Box(Modifier.size(32.dp).clip(RoundedCornerShape(8.dp)).background(it))
                }
            }
        }
    }
}

/** A session of the Agents sample: what its row shows. */
private data class AgentSample(
    val title: String, val status: Status, val preview: String, val time: String,
    val agent: String? = null, val facts: List<RowFact> = emptyList(), val flags: List<RowFlag> = emptyList(),
)

private fun androidx.compose.foundation.lazy.LazyListScope.section(title: String, content: @Composable ColumnScope.() -> Unit) {
    item(title) {
        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            JSectionHeader(title)
            content()
        }
    }
}

/** Kept for previews in other debug code: a transparent spacer the size of the old catalog controls. */
@Suppress("unused")
private val placeholderColor = Color.Transparent
