package persona.feature.chat

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import persona.core.contracts.Session
import persona.core.domain.sort.canSend
import persona.core.designsystem.JAction
import persona.core.designsystem.JActionSheet
import persona.core.designsystem.icons.PhIcons
import persona.core.designsystem.onSecondaryClick
import persona.core.designsystem.stableAlpha

/**
 * A message the long-press menu is open for: [text] is what it says (复制 / 选择 / 引用 / 分享 / 交给 Persona / 作为上下文
 * take it), [editText] what 编辑 puts back in the composer (its photos as attachments again), [user] for your own.
 */
@Immutable
internal data class MessageTarget(val id: String, val text: String, val user: Boolean, val editText: String = text)

/** Which message the long-press menu is open for (iOS `MessageMenu`); the page shows the menu, rows open it. */
@Stable
internal class MessageMenuState {
    var target by mutableStateOf<MessageTarget?>(null)
        private set

    fun open(target: MessageTarget) {
        this.target = target
    }

    fun close() {
        target = null
    }
}

/**
 * The pressable part of a message: a long press (or a right click, docs/android-design.md 3.1) opens the menu, and
 * the message dims while it is open. Taps on links and buttons inside go to them, not here.
 */
@Composable
internal fun Modifier.messageMenu(target: MessageTarget?): Modifier {
    val menu = LocalChatRowActions.current?.menu
    if (menu == null || target == null || target.text.isBlank()) return this
    val haptics = LocalHapticFeedback.current
    val open = remember(menu, target) {
        {
            haptics.performHapticFeedback(HapticFeedbackType.LongPress)
            menu.open(target)
        }
    }
    return this.stableAlpha(if (menu.target?.id == target.id) 0.6f else 1f)
        .onSecondaryClick(open)
        .pointerInput(menu, target) { detectTapGestures(onLongPress = { open() }) }
}

/**
 * The long-press menu as an action sheet (docs/android-design.md 2.3 rule 7): 复制, 选择 (the message as text to
 * select from), 引用, 分享, 交给 Persona, 作为上下文, and 编辑 on your own messages. [writes] false (a read-only
 * conversation) leaves out what would change the composer: 引用, 作为上下文, 编辑.
 */
@Composable
internal fun MessageMenuSheet(
    menu: MessageMenuState,
    writes: Boolean,
    onQuote: (String) -> Unit,
    onEdit: (String) -> Unit,
    onContext: (String) -> Unit,
    onHandToPersona: ((String) -> Unit)?,
    onNotice: (String) -> Unit,
) {
    var selecting by remember { mutableStateOf<String?>(null) }
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val target = menu.target
    if (target != null) JActionSheet(messageMenuActions(target, writes, onHandToPersona != null) { action ->
        when (action) {
            MessageAction.Copy -> {
                clipboard.setText(AnnotatedString(target.text))
                onNotice("已拷贝")
            }
            MessageAction.Select -> { selecting = target.text }
            MessageAction.Quote -> onQuote(target.text)
            MessageAction.Share -> { if (!shareText(context, target.text)) onNotice("没有能分享的应用") }
            MessageAction.HandToPersona -> onHandToPersona?.invoke(target.text)
            MessageAction.Context -> onContext(target.text)
            MessageAction.Edit -> onEdit(target.editText)
        }
    }, onDismiss = menu::close)
    selecting?.let { FullTextReader(it, onDismiss = { selecting = null }, title = "选择文本", plain = true) }
}

/** What the menu offers, in order. */
internal enum class MessageAction(val label: String) {
    Copy("复制"), Select("选择"), Quote("引用"), Share("分享"), HandToPersona("交给 Persona"), Context("作为上下文"), Edit("编辑"),
}

/** The menu's actions for [target]: [writes] for the composer ones, [handOff] when 交给 Persona is offered. */
internal fun messageActions(target: MessageTarget, writes: Boolean, handOff: Boolean): List<MessageAction> = buildList {
    add(MessageAction.Copy)
    add(MessageAction.Select)
    if (writes) add(MessageAction.Quote)
    add(MessageAction.Share)
    if (handOff) add(MessageAction.HandToPersona)
    if (writes) add(MessageAction.Context)
    if (writes && target.user) add(MessageAction.Edit)
}

private fun messageMenuActions(target: MessageTarget, writes: Boolean, handOff: Boolean, run: (MessageAction) -> Unit): List<JAction> =
    messageActions(target, writes, handOff).map { action ->
        JAction(action.label, when (action) {
            MessageAction.Copy -> PhIcons.Copy
            MessageAction.Select -> PhIcons.CursorText
            MessageAction.Quote -> PhIcons.Quotes
            MessageAction.Share -> PhIcons.ShareNetwork
            MessageAction.HandToPersona -> PhIcons.Sparkle
            MessageAction.Context -> PhIcons.Paperclip
            MessageAction.Edit -> PhIcons.PencilSimple
        }) { run(action) }
    }

/**
 * Whether the menu may touch this conversation's composer (引用 / 作为上下文 / 编辑): not in read-only ones (a sub-agent,
 * history). Session capabilities (a source that cannot send) also turn it off once the session carries them.
 */
// A session that takes no messages (a source without send, docs/protocol.md「Codex 来源与能力」) offers no quote, edit or context.
internal fun messageMenuWrites(readOnly: Boolean, session: Session?): Boolean = !readOnly && session?.canSend != false

/** 「引用」: the message quoted at the start of the composer, the cursor after what was already there. */
internal fun ChatComposerState.quote(message: String) {
    beginEdit()
    val text = quoted(message, value.text)
    value = TextFieldValue(text, TextRange(text.length))
    focusRequest++
}

/** 「编辑」: the message back in the composer (after a draft already there), its photos as attachments again. */
internal fun ChatComposerState.edit(message: String) {
    restoreMessage(message)
    focusRequest++
}

/** The message quoted at the start of the composer (「引用」): every line behind `> `, then what was there. */
internal fun quoted(message: String, draft: String): String {
    val quote = message.trim().lines().joinToString("\n") { "> $it" }
    return if (draft.isBlank()) "$quote\n\n" else "$quote\n\n$draft"
}

/** What 「交给 Persona」 sends to the main conversation: the message as context from [title], then the ask. */
internal fun handoffMessage(title: String, text: String): String =
    linkMessage("看看这条消息，告诉我该怎么处理。", listOf(ChatLinkContext("context", "$title · 消息", "", text)))

/** The context 「作为上下文」 adds to the composer: the message, titled by its conversation. */
internal fun messageContext(title: String, text: String): ChatLinkContext = ChatLinkContext("context", "$title · 消息", "", text)

/** The system share sheet with [text]; false when nothing can share it. */
internal fun shareText(context: Context, text: String): Boolean {
    val send = Intent(Intent.ACTION_SEND).apply {
        type = "text/plain"
        putExtra(Intent.EXTRA_TEXT, text)
    }
    val chooser = Intent.createChooser(send, "分享")
    if (context.findActivity() == null) chooser.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    return try {
        context.startActivity(chooser)
        true
    } catch (_: Exception) {
        false
    }
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
