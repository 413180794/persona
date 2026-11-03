package persona.feature.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import persona.core.contracts.Item
import persona.core.contracts.parseTime
import persona.core.designsystem.ChatNoteChip
import persona.core.designsystem.DetailField
import persona.core.designsystem.ErrorCard
import persona.core.designsystem.ErrorTag
import persona.core.designsystem.ErrorTagTone
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JDivider
import persona.core.designsystem.JIconButton
import persona.core.designsystem.JSheet
import persona.core.designsystem.JTextButton
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.LocalAppFeedback
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.ApiErrorInfo
import persona.core.domain.lines.ChatLine
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * A model API error as a card (D6, iOS `APIErrorCard`): the reason in words with its status, 临时故障 or 要处理 and
 * 连续 N 次; tapped, the request id and session to copy, the time and the raw text. 重试 (sends 「继续」) only on the
 * newest card while the agent is idle and the error may pass by itself ([retryLine]).
 */
@Composable
internal fun ApiErrorLine(line: ChatLine.ApiError) {
    val actions = LocalChatRowActions.current
    val info = line.info
    var open by rememberSaveable(line.id) { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current
    val feedback = LocalAppFeedback.current
    val palette = PersonaTheme.palette
    fun copy(text: String) {
        clipboard.setText(AnnotatedString(text))
        feedback?.notice("已复制")
    }
    val retry = actions?.takeIf { it.retryLineId == line.id }?.onRetry
    ErrorCard(
        info.reason, apiErrorTags(info, line.count), open, { open = !open },
        action = if (retry != null) ({ JButton("重试", retry, kind = JButtonKind.Secondary, size = JButtonSize.Medium, icon = PhIcons.ArrowClockwise) }) else null,
    ) {
        info.requestId?.let { id -> DetailField("请求 ID", id) { JIconButton(PhIcons.Copy, "复制请求 ID", { copy(id) }) } }
        info.session?.let { session -> DetailField("Session", session) { JIconButton(PhIcons.Copy, "复制 Session", { copy(session) }) } }
        apiErrorTime(line.item.item.ts)?.let { DetailField("时间", it, mono = false) }
        SelectionContainer {
            Text(info.raw, style = PersonaTheme.type.code, color = palette.ink,
                modifier = Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape).background(palette.sunken).padding(Tokens.Space.m))
        }
        JTextButton("复制原文", { copy(info.raw) }, icon = PhIcons.Copy)
    }
}

/** The facts under an error's reason: its status, whether it may pass by itself, how many times in a row. */
internal fun apiErrorTags(info: ApiErrorInfo, count: Int): List<ErrorTag> = buildList {
    info.status?.let { add(ErrorTag("$it", ErrorTagTone.Danger)) }
    when (info.transient) {
        true -> add(ErrorTag("临时故障"))
        false -> add(ErrorTag("要处理", ErrorTagTone.Caution))
        null -> Unit
    }
    if (count > 1) add(ErrorTag("连续 $count 次"))
}

/** 「11月2日 08:01:05」 for an item's time, in the phone's zone; null without one. */
internal fun apiErrorTime(ts: String?, zone: ZoneId = ZoneId.systemDefault()): String? =
    parseTime(ts)?.let { DateTimeFormatter.ofPattern("M月d日 HH:mm:ss", Locale.ROOT).withZone(zone).format(it) }

/**
 * The card that offers 重试: the conversation's newest line when it is an API error that may pass by itself, the agent
 * is not working and the page can send. [lines] are in reading order (oldest first).
 */
internal fun retryLine(lines: List<ChatLine>, working: Boolean, canSend: Boolean): String? {
    val last = lines.lastOrNull() as? ChatLine.ApiError ?: return null
    return last.id.takeIf { canSend && !working && !last.info.needsAction }
}

/**
 * Claude Code's "while you were away" recap (several in a row are one line): 「离开期间回顾 · 21 分钟前 ›」; tapped,
 * the recaps in full in a sheet, newest first (iOS `RecapDrawer`).
 */
@Composable
internal fun RecapLine(items: List<Item>) {
    if (items.isEmpty()) return
    var open by rememberSaveable(items.first().id) { mutableStateOf(false) }
    val now by rememberNow(60_000)
    val newest = items.last()
    val label = recapLabel(items.size, parseTime(newest.ts), now)
    Box(Modifier.fillMaxWidth()) { ChatNoteChip(label, PhIcons.ClockCountdown, onClick = { open = true }) }
    if (open) JSheet({ open = false }, title = "离开期间回顾", scrollable = true) {
        items.asReversed().forEachIndexed { index, item ->
            if (index > 0) JDivider(Modifier.padding(vertical = Tokens.Space.m))
            parseTime(item.ts)?.let {
                Text("写于 " + relativeTime(it, now), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                Spacer(Modifier.height(Tokens.Space.s))
            }
            ChatMarkdown(item.text.orEmpty(), "recap/${item.id}")
        }
    }
}

/** 「离开期间回顾 · 21 分钟前」, with how many when there are several. */
internal fun recapLabel(count: Int, newest: Instant?, now: Instant): String =
    "离开期间回顾" + (if (count > 1) " $count 条" else "") + (newest?.let { " · " + relativeTime(it, now) } ?: "")
