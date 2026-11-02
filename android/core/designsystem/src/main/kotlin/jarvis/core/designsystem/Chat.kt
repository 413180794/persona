package jarvis.core.designsystem

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.core.designsystem.icons.PhIcon
import jarvis.core.designsystem.icons.PhIcons

/**
 * The bar of a conversation: title, one status line (status, then where it runs), and actions.
 * Floats as a glass capsule in glass; a paper bar in brief; a compact header on the board.
 */
@Composable
fun ChatTopBar(
    title: String,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
    status: Status? = null,
    statusText: String? = null,
    source: String? = null,
    busy: Boolean = false,
    onTitleClick: (() -> Unit)? = null,
    actions: @Composable RowScope.() -> Unit = {},
) {
    val palette = JarvisTheme.palette
    val type = JarvisTheme.type
    val style = JarvisTheme.style
    val tone = status?.let { palette.status(it) }
    val titleBlock: @Composable RowScope.() -> Unit = {
        Column(Modifier.weight(1f).padding(horizontal = 4.dp).then(if (onTitleClick != null) Modifier.clickable(role = Role.Button, onClick = onTitleClick) else Modifier)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (busy) { JSpinner(Modifier.size(14.dp), stroke = 1.8.dp); Spacer(Modifier.width(6.dp)) }
                Text(title, style = if (style == AppStyle.Board) type.headline else type.label.copy(fontSize = androidx.compose.ui.unit.TextUnit(16f, androidx.compose.ui.unit.TextUnitType.Sp)), color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (statusText != null || source != null) Row(verticalAlignment = Alignment.CenterVertically) {
                if (status != null && statusText != null) {
                    when {
                        style == AppStyle.Brief && status == Status.NeedsYou -> MarkerText(statusText, type.micro.copy(fontWeight = FontWeight.Bold))
                        else -> {
                            if (style == AppStyle.Brief && status == Status.Working) JSpinner(Modifier.size(11.dp), stroke = 1.6.dp) else JStatusDot(status, size = 7.dp)
                            Spacer(Modifier.width(5.dp))
                            Text(statusText, style = type.caption, color = if (style == AppStyle.Brief) palette.ink else tone!!.text, maxLines = 1)
                        }
                    }
                } else statusText?.let { Text(it, style = type.caption, color = palette.ink2, maxLines = 1) }
                source?.let {
                    Spacer(Modifier.width(8.dp))
                    Text(it, style = type.caption.copy(fontWeight = FontWeight.Normal), color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
    val back: @Composable () -> Unit = { onBack?.let { JIconButton(if (style == AppStyle.Glass) PhIcons.CaretLeft else PhIcons.ArrowLeft, "返回", it) } }
    when (style) {
        AppStyle.Glass -> Box(modifier.fillMaxWidth().statusBarsPadding().padding(horizontal = 10.dp, vertical = 6.dp)) {
            Row(Modifier.fillMaxWidth().heightIn(min = 60.dp).floatingSurface(RoundedCornerShape(30.dp)).padding(horizontal = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                back(); titleBlock(); actions()
            }
        }
        AppStyle.Brief -> Row(modifier.fillMaxWidth().background(palette.background).statusBarsPadding().heightIn(min = 60.dp).padding(horizontal = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            back(); titleBlock(); actions()
        }
        AppStyle.Board -> Column(modifier.fillMaxWidth().background(palette.background).statusBarsPadding()) {
            Row(Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(horizontal = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                back()
                if (status != null) { JStatusMark(status); Spacer(Modifier.width(8.dp)) }
                titleBlock(); actions()
            }
            Box(Modifier.fillMaxWidth().height(1.dp).background(palette.line))
        }
    }
}

/** The user's message: a tinted glass bubble, a wash bubble, or a bordered box. Right-aligned, at most 86% wide. */
@Composable
fun UserBubbleFrame(modifier: Modifier = Modifier, footer: (@Composable () -> Unit)? = null, content: @Composable ColumnScope.() -> Unit) {
    val palette = JarvisTheme.palette
    val style = JarvisTheme.style
    val shape = when (style) {
        AppStyle.Glass -> RoundedCornerShape(24.dp, 24.dp, 8.dp, 24.dp)
        AppStyle.Brief -> RoundedCornerShape(22.dp, 22.dp, 6.dp, 22.dp)
        AppStyle.Board -> RoundedCornerShape(10.dp)
    }
    Column(modifier.fillMaxWidth().padding(vertical = 6.dp), horizontalAlignment = Alignment.End) {
        Column(
            Modifier.fillMaxWidth(0.86f).wrapContentWidth(Alignment.End).clip(shape)
                .then(when (style) {
                    AppStyle.Glass -> Modifier.background(palette.working.dot.copy(alpha = if (palette.dark) 0.28f else 0.16f))
                        .border(1.dp, Brush.verticalGradient(listOf(palette.strokeTop.copy(alpha = 0.6f), Color.Transparent)), shape)
                    AppStyle.Brief -> Modifier.background(palette.surface)
                    AppStyle.Board -> Modifier.background(palette.surface).border(1.dp, palette.line, shape)
                })
                .padding(horizontal = 16.dp, vertical = 12.dp),
            content = content,
        )
        footer?.let { Spacer(Modifier.height(4.dp)); it() }
    }
}

@Composable
fun UserBubble(text: String, modifier: Modifier = Modifier, time: String? = null) {
    UserBubbleFrame(modifier, footer = time?.let { { Text(it, style = JarvisTheme.type.micro, color = JarvisTheme.palette.ink3) } }) {
        Text(text, style = JarvisTheme.type.body, color = JarvisTheme.palette.ink)
    }
}

/**
 * A folded run of work (tools and thinking between replies). [summary] is one plain sentence such as
 * "做了 6 步，用时 7 分钟"; the steps go in [content] when [expanded].
 */
@Composable
fun WorkGroup(
    summary: String,
    expanded: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
    running: Boolean = false,
    content: @Composable ColumnScope.() -> Unit,
) {
    val palette = JarvisTheme.palette
    val type = JarvisTheme.type
    val header: @Composable () -> Unit = {
        Row(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable(role = Role.Button, onClick = onToggle)
                .semantics { contentDescription = summary + if (expanded) "，已展开" else "，已收起" }
                .padding(horizontal = if (JarvisTheme.style == AppStyle.Brief) 0.dp else 14.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (running) { JSpinner(Modifier.size(15.dp), stroke = 1.8.dp); Spacer(Modifier.width(8.dp)) }
            else if (JarvisTheme.style != AppStyle.Brief) { JIcon(PhIcons.Wrench, null, size = 17.dp, tint = palette.ink2, decorative = true); Spacer(Modifier.width(8.dp)) }
            Text(summary, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = JarvisTheme.style != AppStyle.Brief))
            Spacer(Modifier.width(4.dp))
            JIcon(if (expanded) PhIcons.CaretUp else PhIcons.CaretDown, null, size = 14.dp, tint = palette.ink3)
        }
    }
    when (JarvisTheme.style) {
        AppStyle.Glass -> Column(
            modifier.fillMaxWidth().padding(vertical = 6.dp).clip(RoundedCornerShape(24.dp)).background(palette.surface)
                .border(1.dp, Brush.verticalGradient(listOf(palette.strokeTop, palette.strokeBottom)), RoundedCornerShape(24.dp))
                .animateContentSize(JarvisTheme.motion.size()).padding(vertical = 4.dp),
        ) { header(); if (expanded) content() }
        AppStyle.Brief -> Column(modifier.fillMaxWidth().padding(vertical = 6.dp).animateContentSize(JarvisTheme.motion.size())) {
            header(); if (expanded) { Spacer(Modifier.height(4.dp)); content() }
        }
        AppStyle.Board -> Column(
            modifier.fillMaxWidth().padding(vertical = 6.dp).clip(RoundedCornerShape(10.dp)).background(palette.surface)
                .border(1.dp, palette.line, RoundedCornerShape(10.dp)).animateContentSize(JarvisTheme.motion.size()),
        ) { header(); if (expanded) content() }
    }
}

/** [Unknown]: an older step with no recorded result; neither a success nor a failure. */
enum class WorkState { Done, Running, Failed, Waiting, Unknown }

/**
 * One step of work: a tool call, a file edit, a thought. [verb] (读取 / 编辑 / 运行) is shown on the board;
 * [mono] sets the title in the code face (commands, paths).
 */
@Composable
fun WorkStep(
    icon: PhIcon,
    title: String,
    modifier: Modifier = Modifier,
    verb: String? = null,
    detail: String? = null,
    state: WorkState = WorkState.Done,
    mono: Boolean = false,
    added: Int = 0,
    removed: Int = 0,
    last: Boolean = false,
    onClick: (() -> Unit)? = null,
) {
    val palette = JarvisTheme.palette
    val type = JarvisTheme.type
    val click = if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier
    val diff: @Composable () -> Unit = {
        if (added > 0) Text("+$added", style = type.micro, color = palette.diffAdd)
        if (removed > 0) Text(" −$removed", style = type.micro, color = palette.diffDel)
    }
    val mark: @Composable () -> Unit = {
        when (state) {
            WorkState.Running -> JSpinner(Modifier.size(14.dp), stroke = 1.8.dp)
            WorkState.Failed -> JIcon(PhIcons.XCircle, "失败", size = 16.dp, tint = palette.danger.dot, selected = true)
            WorkState.Waiting -> JIcon(PhIcons.Hand, "等你", size = 16.dp, tint = palette.needsYou.dot, selected = true)
            WorkState.Unknown -> JIcon(PhIcons.Question, "没有结果记录", size = 16.dp, tint = palette.ink3)
            WorkState.Done -> Unit
        }
    }
    val titleStyle = if (mono) type.code else type.callout
    when (JarvisTheme.style) {
        AppStyle.Glass -> Column(modifier.fillMaxWidth().then(click)) {
            Box(Modifier.padding(start = 50.dp).fillMaxWidth().height(1.dp).background(palette.line))
            Row(Modifier.padding(horizontal = 14.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                JIcon(icon, null, size = 20.dp, tint = palette.ink2, decorative = true)
                Spacer(Modifier.width(16.dp))
                Column(Modifier.weight(1f)) {
                    Text(title, style = titleStyle, color = palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    detail?.let { Text(it, style = type.caption.copy(fontWeight = FontWeight.Normal), color = palette.ink3, maxLines = 2, overflow = TextOverflow.Ellipsis) }
                }
                diff(); Spacer(Modifier.width(6.dp)); mark()
                if (state == WorkState.Done && added == 0 && removed == 0 && verb != null && verb.startsWith("运行")) JIcon(PhIcons.Check, null, size = 15.dp, tint = palette.done.dot)
            }
        }
        AppStyle.Brief -> Row(modifier.fillMaxWidth().height(IntrinsicSize.Min).then(click)) {
            Column(Modifier.width(28.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Box(Modifier.size(28.dp).clip(CircleShape).background(palette.surface), contentAlignment = Alignment.Center) {
                    if (state == WorkState.Running) JSpinner(Modifier.size(14.dp), stroke = 1.6.dp) else JIcon(icon, null, size = 15.dp, tint = palette.ink2)
                }
                if (!last) Box(Modifier.width(1.5.dp).weight(1f).background(palette.ink3.copy(alpha = 0.35f)))
            }
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f).padding(top = 4.dp, bottom = if (last) 4.dp else 12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(title, style = titleStyle, color = palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                    Spacer(Modifier.weight(1f)); diff()
                    if (state == WorkState.Failed || state == WorkState.Waiting || state == WorkState.Unknown) { Spacer(Modifier.width(6.dp)); mark() }
                }
                detail?.let {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (state == WorkState.Done && verb?.startsWith("运行") == true) { JIcon(PhIcons.Check, null, size = 13.dp, tint = palette.done.dot); Spacer(Modifier.width(4.dp)) }
                        Text(it, style = type.caption.copy(fontWeight = FontWeight.Normal), color = palette.ink2, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
        AppStyle.Board -> Column(modifier.fillMaxWidth().then(click)) {
            Box(Modifier.fillMaxWidth().height(1.dp).background(palette.line))
            Row(Modifier.padding(horizontal = 12.dp, vertical = 9.dp), verticalAlignment = Alignment.CenterVertically) {
                when (state) {
                    WorkState.Done -> JIcon(PhIcons.Check, null, size = 14.dp, tint = palette.done.dot)
                    else -> mark()
                }
                Spacer(Modifier.width(8.dp))
                verb?.let { Text(it, style = type.caption, color = palette.ink3, maxLines = 1, modifier = Modifier.width(36.dp)) }
                Column(Modifier.weight(1f)) {
                    Text(title, style = titleStyle, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    detail?.let { Text(it, style = type.caption.copy(fontWeight = FontWeight.Normal, fontSize = androidx.compose.ui.unit.TextUnit(12f, androidx.compose.ui.unit.TextUnitType.Sp)), color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                }
                diff()
            }
        }
    }
}

/**
 * The composer frame: [leading] (attach), the [field], [trailing] (send / stop), and an optional [accessory]
 * row above (attachments, contexts, modes). A floating glass capsule in glass; a wash pill in brief; a
 * bordered bar on the board.
 */
@Composable
fun ComposerFrame(
    modifier: Modifier = Modifier,
    leading: (@Composable RowScope.() -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
    accessory: (@Composable ColumnScope.() -> Unit)? = null,
    field: @Composable BoxScope.() -> Unit,
) {
    val palette = JarvisTheme.palette
    when (JarvisTheme.style) {
        AppStyle.Glass -> Column(modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 8.dp).floatingSurface(RoundedCornerShape(28.dp)).padding(6.dp)) {
            accessory?.let { Column(Modifier.padding(start = 8.dp, end = 8.dp, top = 4.dp, bottom = 2.dp), content = it) }
            Row(verticalAlignment = Alignment.Bottom) {
                leading?.invoke(this)
                Box(Modifier.weight(1f).heightIn(min = 44.dp).padding(horizontal = 8.dp, vertical = 11.dp), contentAlignment = Alignment.CenterStart, content = field)
                trailing?.invoke(this)
            }
        }
        AppStyle.Brief -> Column(modifier.fillMaxWidth().background(palette.background).padding(horizontal = 12.dp, vertical = 8.dp)) {
            accessory?.let { Column(Modifier.padding(start = 4.dp, end = 4.dp, bottom = 6.dp), content = it) }
            Row(verticalAlignment = Alignment.Bottom) {
                leading?.let { Row(Modifier.padding(bottom = 2.dp)) { it() }; Spacer(Modifier.width(4.dp)) }
                Row(
                    Modifier.weight(1f).heightIn(min = 48.dp).clip(RoundedCornerShape(24.dp)).background(palette.surface).padding(start = 18.dp, end = 4.dp),
                    verticalAlignment = Alignment.Bottom,
                ) {
                    Box(Modifier.weight(1f).padding(vertical = 13.dp), contentAlignment = Alignment.CenterStart, content = field)
                    trailing?.let { Row(Modifier.padding(vertical = 4.dp)) { it() } }
                }
            }
        }
        // The board's grey under a rule, the field a white ticket on it: no white plate across the bottom.
        AppStyle.Board -> Column(modifier.fillMaxWidth().background(palette.background).drawBehind { drawLine(palette.line, Offset(0f, 0f), Offset(size.width, 0f), 1.dp.toPx()) }.padding(10.dp)) {
            accessory?.let { Column(Modifier.padding(bottom = 8.dp), content = it) }
            Row(verticalAlignment = Alignment.Bottom) {
                leading?.invoke(this)
                Box(
                    Modifier.weight(1f).heightIn(min = 44.dp).clip(RoundedCornerShape(8.dp)).background(palette.surface)
                        .border(1.dp, palette.line, RoundedCornerShape(8.dp)).padding(horizontal = 12.dp, vertical = 11.dp),
                    contentAlignment = Alignment.CenterStart, content = field,
                )
                trailing?.let { Spacer(Modifier.width(8.dp)); it() }
            }
        }
    }
}

/** A borderless text input for [ComposerFrame]. */
@Composable
fun ComposerTextField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    maxLines: Int = 6,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
) {
    val palette = JarvisTheme.palette
    BasicTextField(
        value, onValueChange, modifier.fillMaxWidth(), enabled = enabled, maxLines = maxLines, keyboardOptions = keyboardOptions,
        textStyle = JarvisTheme.type.body.copy(color = palette.ink), cursorBrush = SolidColor(if (JarvisTheme.style == AppStyle.Brief) palette.ink else palette.accent),
        decorationBox = { inner ->
            Box {
                if (value.isEmpty()) Text(placeholder, style = JarvisTheme.type.body, color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
                inner()
            }
        },
    )
}

enum class SendKind { Send, Stop }

/** The send (or stop) button of a composer: a filled circle, or a square on the board. */
@Composable
fun SendButton(onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, kind: SendKind = SendKind.Send, loading: Boolean = false) {
    val palette = JarvisTheme.palette
    val board = JarvisTheme.style == AppStyle.Board
    val shape = if (board) RoundedCornerShape(8.dp) else CircleShape
    val fill = when { kind == SendKind.Stop -> palette.danger.dot; enabled -> palette.action; else -> palette.surfaceStrong }
    val ink = when { kind == SendKind.Stop -> palette.danger.onDot; enabled -> palette.onAction; else -> palette.ink3 }
    Box(
        modifier.size(48.dp).clickable(enabled = enabled && !loading, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = if (kind == SendKind.Stop) "停止" else "发送" },
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.size(if (board) 40.dp else 38.dp).clip(shape).background(fill), contentAlignment = Alignment.Center) {
            when {
                loading -> JSpinner(Modifier.size(16.dp), color = ink, stroke = 2.dp)
                kind == SendKind.Stop -> JIcon(PhIcons.Stop, null, size = 16.dp, tint = ink, selected = true)
                board -> JIcon(PhIcons.PaperPlaneRight, null, size = 18.dp, tint = ink, selected = true)
                else -> JIcon(PhIcons.ArrowUp, null, size = 18.dp, tint = ink, selected = false)
            }
        }
    }
}

/** Jump back to the newest message; [count] new ones arrived while the user read above. */
@Composable
fun JumpToLatest(count: Int, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val palette = JarvisTheme.palette
    val style = JarvisTheme.style
    val shape = if (style == AppStyle.Board) RoundedCornerShape(8.dp) else RoundedCornerShape(50)
    Row(
        modifier.heightIn(min = 44.dp)
            .then(when (style) {
                AppStyle.Glass -> Modifier.floatingSurface(RoundedCornerShape(50))
                AppStyle.Brief -> Modifier.clip(shape).background(palette.action)
                AppStyle.Board -> Modifier.clip(shape).background(palette.surface).border(1.dp, palette.line, shape)
            })
            .clickable(role = Role.Button, onClick = onClick).semantics { contentDescription = if (count > 0) "回到最新，有 $count 条新消息" else "回到最新" }
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val ink = if (style == AppStyle.Brief) palette.onAction else palette.ink
        JIcon(PhIcons.ArrowDown, null, size = 18.dp, tint = ink)
        if (count > 0) { Spacer(Modifier.width(6.dp)); Text("$count", style = JarvisTheme.type.micro.copy(fontWeight = FontWeight.Bold), color = ink) }
    }
}

/** A thin line between turns or a dated separator ("2026-10-31", "这件事从这里开始"). */
@Composable
fun TimelineMark(text: String, modifier: Modifier = Modifier, emphasized: Boolean = false) {
    val palette = JarvisTheme.palette
    Row(modifier.fillMaxWidth().padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.weight(1f).height(1.dp).background(palette.line))
        Text(
            text, style = JarvisTheme.type.micro.copy(fontWeight = if (emphasized) FontWeight.Bold else FontWeight.Medium),
            color = if (emphasized) palette.ink else palette.ink3, modifier = Modifier.padding(horizontal = 12.dp).alpha(1f), maxLines = 1,
        )
        Box(Modifier.weight(1f).height(1.dp).background(palette.line))
    }
}

/**
 * Someone else's message (飞书 conversations, mail threads): an optional avatar, the author and time above a
 * left-aligned bubble. The counterpart of [UserBubbleFrame].
 */
@Composable
fun IncomingBubbleFrame(
    modifier: Modifier = Modifier,
    author: String? = null,
    time: String? = null,
    avatar: (@Composable () -> Unit)? = null,
    footer: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val palette = JarvisTheme.palette
    val style = JarvisTheme.style
    val shape = when (style) {
        AppStyle.Glass -> RoundedCornerShape(24.dp, 24.dp, 24.dp, 8.dp)
        AppStyle.Brief -> RoundedCornerShape(22.dp, 22.dp, 22.dp, 6.dp)
        AppStyle.Board -> RoundedCornerShape(10.dp)
    }
    Row(modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.Top) {
        avatar?.let { it(); Spacer(Modifier.width(10.dp)) }
        Column(Modifier.weight(1f)) {
            if (author != null || time != null) Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(bottom = 4.dp, start = 2.dp)) {
                author?.let { Text(it, style = JarvisTheme.type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false)) }
                time?.let { Spacer(Modifier.width(6.dp)); Text(it, style = JarvisTheme.type.micro, color = palette.ink3, maxLines = 1) }
            }
            Column(
                Modifier.fillMaxWidth(0.9f).wrapContentWidth(Alignment.Start).clip(shape)
                    .then(when (style) {
                        AppStyle.Glass -> Modifier.background(palette.surfaceStrong).border(1.dp, Brush.verticalGradient(listOf(palette.strokeTop.copy(alpha = 0.7f), palette.strokeBottom)), shape)
                        AppStyle.Brief -> Modifier.background(palette.surface)
                        AppStyle.Board -> Modifier.background(palette.surface).border(1.dp, palette.line, shape)
                    })
                    .padding(horizontal = 14.dp, vertical = 10.dp),
                content = content,
            )
            footer?.let { Spacer(Modifier.height(4.dp)); it() }
        }
    }
}
