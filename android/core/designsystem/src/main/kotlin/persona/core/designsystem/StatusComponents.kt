package persona.core.designsystem

import androidx.compose.animation.core.*
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.semantics.*
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcons

/**
 * A status mark: a glowing dot in glass, a dot in brief, a small square holder on the board.
 * Decorative for accessibility; always pair it with visible text.
 */
@Composable
fun JStatusDot(status: Status, modifier: Modifier = Modifier, size: Dp = 8.dp) {
    val tone = PersonaTheme.palette.status(status)
    when (PersonaTheme.style) {
        AppStyle.Glass -> Canvas(modifier.size(size).clearAndSetSemantics {}) {
            drawCircle(Brush.radialGradient(listOf(tone.dot.copy(alpha = 0.55f), Color.Transparent), radius = this.size.minDimension), radius = this.size.minDimension)
            drawCircle(tone.dot)
        }
        AppStyle.Brief -> Box(modifier.size(size).clip(CircleShape).background(tone.dot).clearAndSetSemantics {})
        AppStyle.Board -> Box(modifier.size(size).clip(RoundedCornerShape(2.dp)).background(tone.dot).clearAndSetSemantics {})
    }
}

/** The older dot; same as [JStatusDot]. */
@Composable
fun StatusDot(status: Status, modifier: Modifier = Modifier) = JStatusDot(status, modifier)

/**
 * The leading mark of a session row, larger than a dot: glass glows, brief turns a quiet ring while working and
 * shows a check when done, the board shows a filled square. [animated] lets working sessions move.
 */
@Composable
fun JStatusMark(status: Status, modifier: Modifier = Modifier, animated: Boolean = true) {
    val palette = PersonaTheme.palette
    val tone = palette.status(status)
    val moving = animated && !PersonaTheme.reduceMotion
    when (PersonaTheme.style) {
        AppStyle.Glass -> Box(modifier.size(28.dp), contentAlignment = Alignment.Center) {
            val pulse = if (moving && status == Status.NeedsYou) {
                rememberInfiniteTransition(label = "glow").animateFloat(0.55f, 1f, infiniteRepeatable(tween(1300), RepeatMode.Reverse), label = "pulse").value
            } else 1f
            Canvas(Modifier.size(26.dp).alpha(pulse).clearAndSetSemantics {}) {
                drawCircle(Brush.radialGradient(listOf(tone.dot.copy(alpha = 0.5f), Color.Transparent)))
                drawCircle(tone.dot, radius = 4.5.dp.toPx())
            }
        }
        AppStyle.Brief -> Box(modifier.size(36.dp).clip(CircleShape).background(palette.surface), contentAlignment = Alignment.Center) {
            when (status) {
                Status.Working, Status.Waiting -> JSpinner(Modifier.size(18.dp), color = palette.ink)
                Status.Unread -> JIcon(PhIcons.CheckCircle, null, size = 20.dp, tint = palette.done.dot, selected = true)
                Status.NeedsYou -> Box(Modifier.size(12.dp).clip(CircleShape).background(palette.marker).border(1.5.dp, palette.ink, CircleShape))
                Status.Idle, Status.Unknown, Status.Muted -> JIcon(PhIcons.Check, null, size = 17.dp, tint = palette.ink3)
                Status.Offline -> Box(Modifier.size(8.dp).clip(CircleShape).background(palette.offline.dot))
            }
        }
        AppStyle.Board -> Box(modifier.size(22.dp).clip(RoundedCornerShape(4.dp)).background(tone.dot), contentAlignment = Alignment.Center) {
            when (status) {
                Status.Working, Status.Waiting -> if (moving) JSpinner(Modifier.size(13.dp), color = Color.White, stroke = 1.8.dp)
                    else JIcon(PhIcons.Play, null, size = 12.dp, tint = Color.White, selected = true)
                Status.NeedsYou -> JIcon(PhIcons.Hand, null, size = 13.dp, tint = palette.needsYou.onDot, selected = true)
                Status.Unread -> JIcon(PhIcons.Check, null, size = 13.dp, tint = Color.White)
                else -> JIcon(PhIcons.Checks, null, size = 13.dp, tint = Color.White)
            }
        }
    }
}

/**
 * A state in words with the style's dot before it (已连接, 未连接, 已停用), for a row's trailing side. [problem] puts a
 * warning mark and the words in the danger colour instead (出错).
 */
@Composable
fun JStateLabel(text: String, status: Status, modifier: Modifier = Modifier, problem: Boolean = false) {
    val palette = PersonaTheme.palette
    Row(modifier.semantics(mergeDescendants = true) {}, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
        if (problem) JIcon(PhIcons.WarningCircle, null, size = 14.dp, tint = palette.danger.dot) else JStatusDot(status)
        Text(text, style = PersonaTheme.type.caption, color = if (problem) palette.danger.text else palette.ink2, maxLines = 1, softWrap = false)
    }
}

@Composable
fun StatusLabel(status: Status, modifier: Modifier = Modifier, text: String = status.label) {
    Text(text, modifier, color = PersonaTheme.palette.status(status).text, style = PersonaTheme.type.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
}

enum class PendingStatus(val label: String) {
    Sending("发送中"), Waiting("连上后自动发送"), Unconfirmed("结果待确认"),
    Verifying("正在核对"), NeedsReview("没能确认是否送达"), Refused("没有发出"),
    WaitingForNetwork("等待网络"),
}

@Composable
fun PendingBadge(status: PendingStatus, modifier: Modifier = Modifier, reason: String? = null) {
    val palette = PersonaTheme.palette
    val caution = status != PendingStatus.Sending && status != PendingStatus.Waiting
    val color = if (caution) palette.cautionText else palette.ink2
    val label = if (status == PendingStatus.Refused && !reason.isNullOrBlank()) "${status.label}：$reason" else status.label
    val shape = RoundedCornerShape(if (PersonaTheme.style == AppStyle.Board) 4.dp else 8.dp)
    Row(
        modifier.semantics(mergeDescendants = true) {}.clip(shape).background(if (caution) palette.needsYou.tint else palette.surfaceStrong)
            .padding(horizontal = 8.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically,
    ) {
        if (status == PendingStatus.Sending || status == PendingStatus.Verifying) JSpinner(Modifier.size(14.dp), color = color, stroke = 1.8.dp)
        else JIcon(
            when (status) { PendingStatus.Waiting -> PhIcons.Clock; PendingStatus.Unconfirmed -> PhIcons.HourglassMedium; else -> PhIcons.WarningCircle },
            null, size = 14.dp, tint = color,
        )
        Text(label, style = PersonaTheme.type.caption, color = color, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

enum class StatusLineKind { Ended, MachineOffline, Submitting, Unconfirmed, NeedsYou, Stopping, Working, Waiting }

data class StatusLineState(
    val ended: Boolean = false,
    val replaced: Boolean = false,
    val machineOffline: Boolean = false,
    val lastUpdatedMinutes: Int? = null,
    val submitting: Boolean = false,
    val answering: Boolean = false,
    val unconfirmed: Boolean = false,
    val pendingRequests: Int = 0,
    val blocked: Boolean = false,
    val requestSummary: String? = null,
    val stopping: Boolean = false,
    val working: Boolean = false,
    val workDetail: String? = null,
    val waiting: Boolean = false,
    val waitingDetail: String? = null,
)

data class StatusLineContent(val kind: StatusLineKind, val text: String, val status: Status) {
    val actionable: Boolean get() = kind != StatusLineKind.Submitting && kind != StatusLineKind.Stopping
}

/** Resolves only session state. Hub connectivity belongs in the page title. */
fun resolveStatusLine(state: StatusLineState): StatusLineContent? = with(state) {
    when {
        ended || replaced -> StatusLineContent(StatusLineKind.Ended, if (replaced) "已换成新会话" else "会话已结束", Status.Offline)
        machineOffline -> StatusLineContent(StatusLineKind.MachineOffline,
            "电脑离线，状态可能过时" + (lastUpdatedMinutes?.let { "（${it.coerceAtLeast(0)} 分钟前）" } ?: ""), Status.Offline)
        submitting || answering -> StatusLineContent(StatusLineKind.Submitting,
            if (answering) "正在回传你的选择…" else "正在发送…", Status.Idle)
        unconfirmed -> StatusLineContent(StatusLineKind.Unconfirmed, "上次操作结果待确认", Status.Unknown)
        pendingRequests > 0 || blocked -> StatusLineContent(StatusLineKind.NeedsYou,
            when {
                pendingRequests > 1 -> "等你处理 $pendingRequests 件"
                !requestSummary.isNullOrBlank() -> "等你处理：$requestSummary"
                else -> "等你处理"
            }, Status.NeedsYou)
        stopping -> StatusLineContent(StatusLineKind.Stopping, "已发出中断，等电脑确认", Status.Working)
        working -> StatusLineContent(StatusLineKind.Working,
            "正在工作" + (workDetail?.takeIf { it.isNotBlank() }?.let { "：$it" } ?: ""), Status.Working)
        waiting -> StatusLineContent(StatusLineKind.Waiting, waitingDetail?.takeIf { it.isNotBlank() } ?: "等待下一次任务", Status.Waiting)
        else -> null
    }
}

/**
 * One line of session state above the composer. Extra facts (循环, 后台 2, 排队 1) ride along as [chips] instead
 * of stacking more rows.
 */
@Composable
fun StatusLine(
    state: StatusLineState,
    modifier: Modifier = Modifier,
    onClick: ((StatusLineKind) -> Unit)? = null,
    chips: List<String> = emptyList(),
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    val line = resolveStatusLine(state)
    if (line == null && chips.isEmpty()) return
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val status = line?.status ?: Status.Idle
    val tone = palette.status(status)
    val caution = line?.kind == StatusLineKind.Unconfirmed
    val color = if (caution) palette.cautionText else when (style) {
        AppStyle.Brief -> palette.ink
        else -> tone.text
    }
    val click = if (onClick != null && (line == null || line.actionable)) Modifier.clickable(role = Role.Button) { onClick(line?.kind ?: StatusLineKind.Waiting) } else Modifier
    val shape = RoundedCornerShape(if (style == AppStyle.Board) 8.dp else 50.dp)
    val frame = when (style) {
        AppStyle.Glass -> Modifier.clip(shape).background(tone.tint).border(1.dp, palette.strokeBottom, shape)
        AppStyle.Brief -> Modifier.clip(shape)
        AppStyle.Board -> Modifier.clip(shape).background(palette.surface).border(1.dp, palette.line, shape)
    }
    Row(
        modifier.fillMaxWidth().padding(horizontal = if (style == AppStyle.Brief) 6.dp else 10.dp, vertical = 4.dp).then(frame).then(click)
            .heightIn(min = 40.dp).padding(horizontal = 12.dp, vertical = 8.dp)
            .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically,
    ) {
        if (line != null) {
            when {
                caution -> JIcon(PhIcons.HourglassMedium, null, size = 16.dp, tint = color)
                style == AppStyle.Brief && line.kind == StatusLineKind.NeedsYou -> Box(Modifier.size(10.dp).clip(CircleShape).background(palette.marker).border(1.5.dp, palette.ink, CircleShape))
                style == AppStyle.Brief && (line.kind == StatusLineKind.Working || line.kind == StatusLineKind.Stopping) -> JSpinner(Modifier.size(14.dp), stroke = 1.8.dp)
                style == AppStyle.Board -> JStatusMark(status, animated = line.kind == StatusLineKind.Working)
                else -> JStatusDot(status, size = 8.dp)
            }
            Text(line.text, color = color, style = PersonaTheme.type.caption, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
        }
        if (chips.isNotEmpty()) {
            Spacer(Modifier.weight(1f))
            chips.forEach { Text(it, style = PersonaTheme.type.micro, color = palette.ink2, maxLines = 1) }
        }
        trailing?.invoke(this)
        if (onClick != null && line?.actionable != false) JIcon(PhIcons.CaretRight, null, size = 14.dp, tint = palette.ink3)
    }
}
