package jarvis.android.ui.chat

import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.JTheme
import android.graphics.Bitmap
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.sizeIn
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.rounded.OpenInNew
import androidx.compose.material.icons.rounded.BrokenImage
import androidx.compose.material.icons.rounded.Cancel
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.ContentCopy
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.ExpandMore
import androidx.compose.material.icons.rounded.Layers
import androidx.compose.material.icons.rounded.RadioButtonUnchecked
import androidx.compose.material.icons.automirrored.rounded.Subject
import androidx.compose.material.icons.rounded.TripOrigin
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import jarvis.android.data.*
import jarvis.android.ui.*
import jarvis.android.ui.media.*
import jarvis.android.ui.files.DiffText
import jarvis.android.ui.files.DiffLine
import jarvis.android.ui.files.DiffContent
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

private fun JsonObject?.str(k: String): String? = ((this?.get(k)) as? JsonPrimitive)?.takeIf { it.isString }?.content
private fun ToolCall.input(): JsonObject? = use.toolInput as? JsonObject

/** A run of tool calls outside a work line (iOS `ToolGroupView`): more than two fold to 「N 个步骤 · 运行 5 · 读取 3」, the last always shows. */
@Composable
fun ToolGroup(steps: List<ToolStep>, working: Boolean) {
    val calls = remember(steps) { steps.mapNotNull { (it as? ToolStep.Call)?.call } }
    var expanded by remember(steps.first().id) { mutableStateOf(false) }
    val c = chatColors
    Column(Modifier.fillMaxWidth().animateContentSize(), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        if (calls.size > 2 && !expanded) {
            Row(
                Modifier.fillMaxWidth().clip(CircleShape).background(c.fillWell).clickable { expanded = true }.padding(horizontal = JSpace.m, vertical = JSpace.s),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
            ) {
                Icon(JIcons.space, null, Modifier.size(JSize.markS), tint = c.secondaryLabel)
                Text("${calls.size} 个步骤", style = JTheme.type.labelMedium, color = c.secondaryLabel, maxLines = 1)
                Text(ToolText.runSummary(calls), style = JTheme.type.label, color = c.tertiaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Icon(JIcons.expand, null, Modifier.size(JSize.markM), tint = c.secondaryLabel)
            }
            calls.lastOrNull()?.let { ToolCallRow(it, working, autoExpand = working && it.isRunning) }
        } else {
            steps.forEach { s ->
                when (s) {
                    is ToolStep.Call -> ToolCallRow(s.call, working)
                    is ToolStep.Thought -> if (s.item.kind == "notice") NoticeRow(s.item) else ThinkingRow(s.item)
                }
            }
            if (calls.size > 2) {
                Text("收起", style = JTheme.type.labelMedium, color = c.accent, maxLines = 1,
                    modifier = Modifier.padding(start = JSpace.m).clip(RoundedCornerShape(JRadius.badge)).clickable { expanded = false }.padding(JSpace.xs))
            }
        }
    }
}

/** Where a tool row's words start inside a work card (inset + 24 dp icon + 10): separators start there. */
val WorkInset = JSpace.m
val WorkTextColumn = WorkInset + JSize.icon + JSpace.m

/**
 * One tool call (iOS `ToolCallRow`): icon (a spinner while it runs), verb, subject, chevron; tap for its detail.
 * [grouped] = a row of an open work line's card: no tile of its own, at least 44 dp tall.
 */
@Composable
fun ToolCallRow(call: ToolCall, working: Boolean, grouped: Boolean = false, autoExpand: Boolean = false) {
    val c = chatColors
    var expanded by remember(call.use.id) { mutableStateOf<Boolean?>(null) }
    val open = expanded ?: autoExpand
    val running = call.isRunning && working
    val tint = when {
        call.isError -> c.danger
        running -> chatColors.working
        else -> c.secondaryLabel
    }
    val pad = if (grouped) WorkInset else 10.dp
    Column(
        Modifier.fillMaxWidth().then(if (grouped) Modifier else Modifier.clip(RoundedCornerShape(JRadius.card)).background(c.fillWell)).animateContentSize(),
    ) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = if (grouped) 44.dp else 38.dp).clickable { expanded = !open }.padding(horizontal = pad, vertical = JSpace.sm),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            // On the timeline the symbol sits on the line: the page's color under its tint hides the line there.
            Box(Modifier.size(JSize.icon).clip(CircleShape).then(if (grouped && workTimeline) Modifier.background(c.page) else Modifier)
                .background(tint.copy(alpha = 0.14f)), contentAlignment = Alignment.Center) {
                if (running) CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = tint)
                else Icon(ToolText.icon(call.name), null, Modifier.size(JSize.markXS), tint = tint)
            }
            Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                Text(ToolText.verb(call.name), style = if (grouped && workTimeline) JTheme.type.secondaryMedium else JTheme.type.secondaryStrong, color = c.label, maxLines = 1)
                Text(remember(call.use.id) { ToolText.subject(call.use) }, style = JTheme.type.secondary, color = c.secondaryLabel, maxLines = 1,
                    overflow = TextOverflow.Ellipsis)
            }
            if (call.isError) Icon(JIcons.errorOutline, null, Modifier.size(JSize.mark), tint = c.danger)
            Icon(JIcons.disclosure, if (open) "收起" else "展开", Modifier.size(JSize.markM).rotate(if (open) 90f else 0f), tint = c.tertiaryLabel)
        }
        // On the timeline what opens under a step starts at the words' column, right of the line.
        val detailStart = if (grouped && workTimeline) WorkTextColumn else pad
        if (ToolText.baseName(call.name) == "TodoWrite") {
            TodoList(call.input()?.get("todos") as? JsonArray, Modifier.padding(start = detailStart, end = pad, bottom = JSpace.s))
        } else if (open) {
            ToolDetail(call, working, Modifier.padding(start = detailStart, end = pad, bottom = JSpace.m))
        }
    }
}

/** What a call did (iOS `ToolDetail`): the command, the diff, the path, the plan, the question…, then its output. */
@Composable
fun ToolDetail(call: ToolCall, working: Boolean, modifier: Modifier = Modifier) {
    val c = chatColors
    val base = ToolText.baseName(call.name)
    val i = call.input()
    val env = LocalChatEnv.current
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
        when (base) {
            "Bash" -> CodeBlock("$ " + i.str("command").orEmpty(), maxLines = 12)
            "Edit" -> {
                val content = remember(call.use.id) {
                    i.str("diff")?.let { DiffText.unified(it) } ?: DiffText.edit(i.str("old_string").orEmpty(), i.str("new_string").orEmpty())
                }
                DiffHeader(i.str("file_path"), content.adds, content.dels)
                DiffView(content)
            }
            "MultiEdit" -> {
                val diffs = remember(call.use.id) {
                    (i?.get("edits") as? JsonArray).orEmpty().map { e -> (e as? JsonObject).let { DiffText.edit(it.str("old_string").orEmpty(), it.str("new_string").orEmpty()) } }
                }
                DiffHeader(i.str("file_path"), diffs.sumOf { it.adds }, diffs.sumOf { it.dels })
                diffs.forEach { DiffView(it) }
            }
            "Write" -> {
                val content = remember(call.use.id) { DiffText.edit("", i.str("content").orEmpty()) }
                DiffHeader(i.str("file_path"), content.adds, content.dels)
                DiffView(content)
            }
            "Read", "NotebookEdit" -> (i.str("file_path") ?: i.str("notebook_path"))?.let { PathLink(it) }
            "ExitPlanMode" -> Markdown(i.str("plan").orEmpty())
            "AskUserQuestion" -> (i?.get("questions") as? JsonArray).orEmpty().forEach { q ->
                Text(((q as? JsonObject).str("question")).orEmpty(), style = JTheme.type.captionMedium, color = c.label)
            }
            "Task", "Agent" -> Text(i.str("prompt").orEmpty(), style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 8, overflow = TextOverflow.Ellipsis)
            else -> if (i != null && i.isNotEmpty()) CodeBlock(remember(call.use.id) { pretty(i) }, maxLines = 14)
        }
        val result = call.result
        if (result != null && (base != "Edit" || result.isError == true)) {
            ResultBlock(result)
        } else if (call.isRunning && working) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium)
                Text("运行中…", style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1)
            }
        }
        if (call.images.isNotEmpty() && env != null) {
            val all = remember(call.images) { call.images.map { imageOf(env.key, it) } }
            all.forEach { ChatThumb(it, local = all, modifier = Modifier.fillMaxWidth()) }
        }
    }
}

/** A path in a tool's input; opens the file on the session's machine. */
@Composable
fun PathLink(path: String) {
    val c = chatColors
    val env = LocalChatEnv.current
    Row(
        Modifier.clip(RoundedCornerShape(JRadius.badge)).clickable(enabled = env?.canOpenFiles == true) { env?.openFile(path) }.padding(vertical = JSpace.xxs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
    ) {
        Icon(fileIcon(path), null, Modifier.size(JSize.markXS), tint = if (env?.canOpenFiles == true) c.accent else c.secondaryLabel)
        Text(path, style = JTheme.type.code, color = if (env?.canOpenFiles == true) c.accent else c.secondaryLabel,
            maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
        if (env?.canOpenFiles == true) Icon(JIcons.openExternal, null, Modifier.size(JSize.markXS), tint = c.accent)
    }
}

/** A tool's output: 「出错了」 above it (never over its first line), at most 10 lines, 看全文 when the page cut it. */
@Composable
fun ResultBlock(result: Item) {
    val c = chatColors
    val error = result.isError == true
    val text = result.cutText
    var full by remember(result.id) { mutableStateOf(false) }
    if (text.isEmpty() && result.fullSize == null) {
        Text(if (error) "失败" else "完成，无输出", style = JTheme.type.label, color = if (error) c.danger else c.secondaryLabel)
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        if (error) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Icon(JIcons.clear, null, Modifier.size(JSize.markS), tint = c.danger)
                Text("出错了", style = JTheme.type.labelStrong, color = c.danger, maxLines = 1)
            }
        }
        CodeBlock(text.trimEnd(), maxLines = 10)
        result.fullSize?.let { size ->
            Row(
                Modifier.clip(RoundedCornerShape(JRadius.badge)).clickable { full = true }.padding(vertical = JSpace.xs),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
            ) {
                Icon(JIcons.output, null, Modifier.size(JSize.markS), tint = c.accent)
                Text("已截断，看全文（${formatBytes(size.toLong())}）", style = JTheme.type.labelMedium, color = c.accent, maxLines = 1)
            }
        }
    }
    if (full) FullOutputDialog(result.id) { full = false }
}

/** A tool's whole output, fetched when opened (pages carry its first 4 KB): monospaced, scrolls both ways (iOS `FullOutputSheet`). */
@Composable
fun FullOutputDialog(itemId: String, onDismiss: () -> Unit) {
    val env = LocalChatEnv.current ?: return
    val ctx = LocalContext.current
    var text by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(itemId) {
        runCatching { env.hub.api.fullItem(env.key, itemId).text.orEmpty() }.onSuccess { text = it }.onFailure { if (!it.isCancellation()) error = it.shownText() }
    }
    FullScreenPage(
        title = "工具输出",
        onBack = onDismiss,
        actions = {
            text?.let { t ->
                IconButton(onClick = { MediaShare.copyText(ctx, t, "工具输出"); Toasts.show("已拷贝", ToastStyle.SUCCESS) }) {
                    Icon(JIcons.copy, contentDescription = "拷贝全文")
                }
            }
        },
    ) { pad ->
        Box(Modifier.fillMaxSize().padding(pad).background(chatColors.codeBackground)) {
            val t = text
            when {
                t != null -> jarvis.android.ui.files.MonoLines(jarvis.android.ui.files.numberedLines(t), Modifier.fillMaxSize())
                error != null -> Text(error!!, color = chatColors.danger, modifier = Modifier.align(Alignment.Center).padding(JSpace.xl))
                else -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(JSize.icon), strokeWidth = JSize.rule) }
            }
        }
    }
}


/** The file a diff is in: name, folder, +N −N; a tap opens it. */
@Composable
fun DiffHeader(path: String?, adds: Int, dels: Int) {
    if (path.isNullOrEmpty()) return
    val c = chatColors
    val env = LocalChatEnv.current
    val canOpen = env?.canOpenFiles == true
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.badge)).clickable(enabled = canOpen) { env?.openFile(path) }.padding(vertical = JSpace.xxs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        Icon(fileIcon(path), null, Modifier.size(JSize.markXS), tint = c.secondaryLabel)
        Text(path.substringAfterLast('/'), style = JTheme.type.captionStrong, color = c.label, maxLines = 1)
        Text(path.substringBeforeLast('/', ""), style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f))
        if (adds > 0) Text("+$adds", style = JTheme.type.captionStrong, color = c.diffAdd)
        if (dels > 0) Text("−$dels", style = JTheme.type.captionStrong, color = c.diffDel)
        if (canOpen) Icon(JIcons.openExternal, "查看文件", Modifier.size(JSize.markXS), tint = c.accent)
    }
}

/**
 * The chat's one diff (iOS `DiffView`): only the ± sign and the line's background are colored, the code stays the
 * body color; long lines scroll sideways with a fade on the right while there is more; past 40 lines it shows the
 * start and 展开全部 N 行.
 */
@Composable
fun DiffView(content: DiffContent) {
    val c = chatColors
    var full by remember(content) { mutableStateOf(false) }
    val max = 40
    val clipped = !full && content.lines.size > max
    val shown = if (clipped) content.lines.take(max) else content.lines
    val scroll = rememberScrollState()
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        // Every line as wide as the longest (or the box): the colored backgrounds run the full width.
        BoxWithConstraints(Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.control)).background(c.codeBackground)) {
            val boxWidth = maxWidth
            Column(Modifier.horizontalScroll(scroll).width(IntrinsicSize.Max).widthIn(min = boxWidth).padding(vertical = JSpace.sm)) {
                shown.forEach { l ->
                    val (sign, color, fill) = when (l.kind) {
                        DiffLine.Kind.ADD -> Triple("+", c.diffAdd, c.diffAddFill)
                        DiffLine.Kind.DEL -> Triple("−", c.diffDel, c.diffDelFill)
                        else -> Triple(" ", c.secondaryLabel, Color.Transparent)
                    }
                    val quiet = l.kind == DiffLine.Kind.HUNK || l.kind == DiffLine.Kind.INFO
                    Row(Modifier.fillMaxWidth().background(fill).padding(horizontal = JSpace.s, vertical = JSize.line)) {
                        Text(sign, style = JTheme.type.code, color = color, modifier = Modifier.width(JSize.markXS))
                        Text(l.text.ifEmpty { " " }, style = JTheme.type.code,
                            color = if (quiet) c.secondaryLabel else c.label, softWrap = false)
                    }
                }
            }
            if (scroll.canScrollForward) {
                Box(Modifier.matchParentSize()) {
                    Box(
                        Modifier.align(Alignment.CenterEnd).width(JSize.wellXS).fillMaxHeight()
                            .background(Brush.horizontalGradient(listOf(c.codeBackground.copy(alpha = 0f), c.codeBackground))),
                    )
                }
            }
        }
        if (clipped) {
            Text("展开全部 ${content.lines.size} 行", style = JTheme.type.labelMedium, color = c.accent, maxLines = 1,
                modifier = Modifier.clip(RoundedCornerShape(JRadius.badge)).clickable { full = true }.padding(vertical = JSpace.xs, horizontal = JSpace.xxs))
        }
    }
}

/** TodoWrite rendered as the checklist it is. */
@Composable
fun TodoList(todos: JsonArray?, modifier: Modifier = Modifier) {
    val c = chatColors
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        todos.orEmpty().forEach { t ->
            val o = t as? JsonObject
            val status = o.str("status") ?: "pending"
            val done = status == "completed"
            val doing = status == "in_progress"
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                Icon(
                    when { done -> JIcons.success; doing -> JIcons.inProgress; else -> JIcons.unchecked },
                    contentDescription = when { done -> "已完成"; doing -> "进行中"; else -> "待办" },
                    tint = when { done -> c.positive; doing -> chatColors.working; else -> c.secondaryLabel },
                    modifier = Modifier.padding(top = JSize.line).size(JSize.markM),
                )
                Text(
                    if (doing) o.str("activeForm") ?: o.str("content").orEmpty() else o.str("content").orEmpty(),
                    style = JTheme.type.caption,
                    lineHeight = 18.sp,
                    color = if (done) c.secondaryLabel else c.label,
                    textDecoration = if (done) TextDecoration.LineThrough else null,
                )
            }
        }
    }
}

/**
 * A picture in the conversation (iOS `Base64Image`): holds its shape while it downloads (sizes come with the page,
 * so the list does not jump), at most 280 dp tall; tap for the viewer.
 */
@Composable
fun ChatThumb(img: ViewerImage, local: List<ViewerImage> = listOf(img), modifier: Modifier = Modifier, width: Int? = null, height: Int? = null) {
    val c = chatColors
    val env = LocalChatEnv.current
    var attempt by remember(img) { mutableIntStateOf(0) }
    val state by produceState<Any?>(ChatImageLoader.cached(img, 900), img, attempt) {
        if (value is Bitmap) return@produceState
        value = env?.hub?.let { ChatImageLoader.load(it, img, 900) } ?: false
    }
    val b = state as? Bitmap
    val ratio = when {
        b != null -> b.width.toFloat() / b.height.coerceAtLeast(1)
        (width ?: 0) > 0 && (height ?: 0) > 0 -> width!!.toFloat() / height!!
        else -> 1.4f
    }.coerceIn(0.25f, 4f)
    val shape = RoundedCornerShape(JRadius.control)
    Box(modifier.sizeIn(maxHeight = 280.dp), contentAlignment = Alignment.CenterStart) {
        Box(
            Modifier.aspectRatio(ratio, matchHeightConstraintsFirst = ratio < 1f).clip(shape).background(c.fillWell)
                .border(0.5.dp, c.cardStroke, shape)
                .clickable(enabled = b != null || state == false) { if (b != null) env?.openImage(img, local) else attempt++ },
            contentAlignment = Alignment.Center,
        ) {
            when {
                b != null -> Image(remember(b) { b.asImageBitmap() }, "图片，点按查看大图", contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
                state == false -> Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Icon(JIcons.brokenImage, null, tint = c.secondaryLabel)
                    Text("加载失败，点一下重试", style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1)
                }
                else -> CircularProgressIndicator(Modifier.size(JSize.badge), strokeWidth = JSize.rule)
            }
        }
    }
}

/** A square thumbnail (sent photos, the composer's pictures): the picture fills and is cropped to it. */
@Composable
fun SquareThumb(img: ViewerImage, side: androidx.compose.ui.unit.Dp, onClick: (() -> Unit)?) {
    val c = chatColors
    val hub = LocalChatEnv.current?.hub
    val bmp by produceState<Bitmap?>(ChatImageLoader.cached(img, 400), img) { if (value == null) value = hub?.let { ChatImageLoader.load(it, img, 400) } }
    val shape = RoundedCornerShape(JRadius.control)
    Box(
        Modifier.size(side).clip(shape).background(c.fillChip).border(0.5.dp, c.label.copy(alpha = 0.1f), shape)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier),
        contentAlignment = Alignment.Center,
    ) {
        val b = bmp
        if (b != null) Image(remember(b) { b.asImageBitmap() }, "图片", contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        else CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule)
    }
}

/** Monospaced text in the code fill (tool inputs and outputs): scrolls sideways; long ones show [maxLines] and 展开全部. */
@Composable
fun CodeBlock(code: String, modifier: Modifier = Modifier, maxLines: Int = 20) {
    val c = chatColors
    var full by remember(code) { mutableStateOf(false) }
    val lines = remember(code) { code.split('\n') }
    val clipped = !full && lines.size > maxLines
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        Box(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.control)).background(c.codeBackground)
                .horizontalScroll(rememberScrollState()).padding(JSpace.m),
        ) {
            Text(
                if (clipped) remember(code, maxLines) { lines.take(maxLines).joinToString("\n") } else code,
                fontFamily = FontFamily.Monospace,
                style = JTheme.type.label,
                lineHeight = 16.sp,
                color = c.label,
                softWrap = false,
            )
        }
        if (clipped) {
            Text(
                "展开全部 ${lines.size} 行",
                style = JTheme.type.label,
                fontWeight = FontWeight.Medium,
                color = c.accent,
                maxLines = 1,
                modifier = Modifier.clip(RoundedCornerShape(JRadius.badge)).clickable { full = true }.padding(vertical = JSpace.xs, horizontal = JSpace.xxs),
            )
        }
    }
}
