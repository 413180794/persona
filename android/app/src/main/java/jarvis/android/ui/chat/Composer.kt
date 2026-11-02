package jarvis.android.ui.chat

import android.content.Intent
import android.speech.RecognizerIntent
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.widthIn
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.platform.LocalContext
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateContentSize
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.Haptics
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.hapticView
import jarvis.android.ui.design.surface

/** What a composer's buttons do; the callers keep the drafts, uploads and send checks. */
class ComposerActions(
    val send: () -> Unit,
    /** Set while the agent works: a stop button takes send's place while nothing is typed. */
    val stop: (() -> Unit)? = null,
    /** The + menu's items (照片, 文件…); `close` shuts the menu. */
    val attach: (@Composable ColumnScope.(close: () -> Unit) -> Unit)? = null,
    /** The gauge menu's items (权限和思考强度), shown in the expanded shell composer. */
    val modes: (@Composable ColumnScope.(close: () -> Unit) -> Unit)? = null,
    /** The voice button: the hub's voice conversation, when there is one (the shell composer). */
    val voice: (() -> Unit)? = null,
    /** The mic: dictation into the box ([rememberDictation]); null when the phone has no recognizer. */
    val dictate: (() -> Unit)? = null,
)

/**
 * The Agents chat's composer (iOS `ComposerRow`): + on its own round surface, the field in a capsule that grows to
 * six lines, then send (the accent; faded with nothing to send) — or, while the agent works and nothing is typed, a
 * red stop.
 */
@Composable
fun ChatComposer(
    text: String, onText: (String) -> Unit, placeholder: String, canSend: Boolean, actions: ComposerActions,
    modifier: Modifier = Modifier, focus: FocusRequester = remember { FocusRequester() }, enabled: Boolean = true,
) {
    val c = JTheme.colors
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
        if (actions.attach != null) AttachButton(actions.attach, floating = true, enabled = enabled)
        Box(
            Modifier.weight(1f).heightIn(min = JSize.tapTarget).surface(RoundedCornerShape(JRadius.panel), floating = true)
                .padding(horizontal = JSpace.l, vertical = JSpace.m),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (text.isEmpty()) Text(placeholder, style = JTheme.type.body, color = c.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            BasicTextField(
                text, onText, enabled = enabled, maxLines = 6,
                textStyle = JTheme.type.body.copy(color = c.label), cursorBrush = SolidColor(c.accent),
                modifier = Modifier.fillMaxWidth().focusRequester(focus).semantics { contentDescription = placeholder },
            )
        }
        val stop = actions.stop
        val dictate = actions.dictate
        // Nothing typed: the mic where send will be (a stop beside it while the agent works).
        if (dictate != null && !canSend) PlainRound(JIcons.dictate, "听写", dictate)
        when {
            stop != null && text.isEmpty() && !canSend -> RoundButton(JIcons.interrupt, "中断", stop, fill = c.danger, words = Color.White)
            dictate != null && !canSend -> {}
            else -> RoundButton(JIcons.send, "发送", actions.send, fill = c.accent, words = Color.White, enabled = canSend && enabled)
        }
    }
}

/**
 * The shell's composer (iOS `ShellComposerBar`, ChatGPT's): one line — [+] 问 Jarvis… [mic] [voice] — that grows into
 * a card while typing: the field on top, a row under it: [+] … [modes] [mic] [send]. The field is one view in both shapes,
 * so focus and the IME's composing text survive the change.
 */
@Composable
fun ShellComposer(
    text: String, onText: (String) -> Unit, placeholder: String, canSend: Boolean, actions: ComposerActions,
    modifier: Modifier = Modifier, focus: FocusRequester = remember { FocusRequester() },
    hasAttachments: Boolean = false, enabled: Boolean = true, modesWarn: Boolean = false,
) {
    val c = JTheme.colors
    val shell = JTheme.shell
    var focused by remember { mutableStateOf(false) }
    val expanded = focused || text.isNotEmpty() || hasAttachments
    val shape = RoundedCornerShape(ComposerRadius)
    Column(
        modifier.fillMaxWidth()
            .shadow(JSpace.m, shape, clip = false, ambientColor = Color.Black.copy(alpha = 0.08f), spotColor = Color.Black.copy(alpha = 0.08f))
            .clip(shape).background(shell.composer).border(JSize.hairline, shell.composerStroke, shape)
            .clickable(remember { MutableInteractionSource() }, indication = null) { runCatching { focus.requestFocus() } }
            .animateContentSize()
            .padding(horizontal = if (expanded) 0.dp else JSpace.xs, vertical = if (expanded) 0.dp else JSpace.xxs),
    ) {
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            if (!expanded && actions.attach != null) AttachButton(actions.attach, floating = false, enabled = enabled)
            Box(
                Modifier.weight(1f).heightIn(min = JSize.tapTarget)
                    .padding(start = if (expanded) JSpace.l else if (actions.attach == null) JSpace.m else JSpace.xs, end = if (expanded) JSpace.l else JSpace.xs, top = if (expanded) JSpace.l else 0.dp),
                contentAlignment = Alignment.CenterStart,
            ) {
                if (text.isEmpty()) Text(placeholder, style = JTheme.type.body, color = c.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                BasicTextField(
                    text, onText, enabled = enabled, maxLines = if (expanded) 8 else 1,
                    textStyle = JTheme.type.body.copy(color = c.label), cursorBrush = SolidColor(c.accent),
                    modifier = Modifier.fillMaxWidth().focusRequester(focus).onFocusChanged { focused = it.isFocused }
                        .semantics { contentDescription = placeholder },
                )
            }
            if (!expanded) {
                actions.dictate?.let { PlainRound(JIcons.dictate, "听写", it) }
                actions.voice?.let { PlainRound(JIcons.voice, "语音对话", it) }
                actions.stop?.let { RoundButton(JIcons.interrupt, "中断", it, fill = shell.ink, words = shell.inkText) }
            }
        }
        AnimatedVisibility(expanded, enter = fadeIn(), exit = fadeOut()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.s).padding(bottom = JSpace.s), verticalAlignment = Alignment.CenterVertically) {
                if (actions.attach != null) AttachButton(actions.attach, floating = false, enabled = enabled)
                Spacer(Modifier.weight(1f))
                actions.modes?.let { items -> MenuRound(JIcons.effort, "权限和思考强度", tint = if (modesWarn) c.danger else c.label, items = items) }
                actions.dictate?.let { PlainRound(JIcons.dictate, "听写", it) }
                val stop = actions.stop
                when {
                    canSend -> RoundButton(JIcons.send, "发送", actions.send, fill = shell.ink, words = shell.inkText, enabled = enabled)
                    stop != null -> RoundButton(JIcons.interrupt, "中断", stop, fill = shell.ink, words = shell.inkText)
                    else -> RoundButton(JIcons.send, "发送", actions.send, fill = shell.ink, words = shell.inkText, enabled = false)
                }
            }
        }
    }
}

/** The composer field's corner (a capsule at one line, a card once grown). */
val ComposerRadius = JSize.well

/** The + and its menu. [floating]: on its own round surface (the Agents chat), else bare inside the composer. */
@Composable
private fun AttachButton(items: @Composable ColumnScope.(close: () -> Unit) -> Unit, floating: Boolean, enabled: Boolean) {
    var open by remember { mutableStateOf(false) }
    Box {
        Box(
            Modifier.size(JSize.tapTarget).then(if (floating) Modifier.surface(CircleShape, floating = true) else Modifier.clip(CircleShape))
                .clickable(enabled = enabled, role = Role.Button) { open = true }.semantics { contentDescription = "添加照片或文件" }
                .alpha(if (enabled) 1f else 0.4f),
            contentAlignment = Alignment.Center,
        ) { Icon(JIcons.add, null, Modifier.size(JSize.icon), tint = if (floating) JTheme.colors.accent else JTheme.colors.label) }
        DropdownMenu(open, onDismissRequest = { open = false }) { items { open = false } }
    }
}

/** A bare round symbol that opens a menu. */
@Composable
private fun MenuRound(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, tint: Color, items: @Composable ColumnScope.(close: () -> Unit) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        PlainRound(icon, label, { open = true }, tint = tint)
        DropdownMenu(open, onDismissRequest = { open = false }) { items { open = false } }
    }
}

@Composable
private fun PlainRound(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, onClick: () -> Unit, tint: Color = JTheme.colors.label) {
    Box(
        Modifier.size(JSize.tapTarget).clip(CircleShape).clickable(role = Role.Button, onClick = onClick).semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) { Icon(icon, null, Modifier.size(JSize.iconS), tint = tint) }
}

/** Send / stop: a filled disc (36) in a 44 target; it pops in and out. */
@Composable
fun RoundButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, onClick: () -> Unit,
    fill: Color, words: Color, enabled: Boolean = true,
) {
    val view = hapticView()
    Box(
        Modifier.size(JSize.tapTarget).clip(CircleShape)
            .clickable(enabled = enabled, role = Role.Button) { Haptics.tap(view); onClick() }
            .semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier.size(JSize.wellL).clip(CircleShape).background(if (enabled) fill else JTheme.colors.fillPressed),
            contentAlignment = Alignment.Center,
        ) { Icon(icon, null, Modifier.size(JSize.iconXS), tint = if (enabled) words else JTheme.colors.tertiaryText) }
    }
}

/**
 * Typed "/": Jarvis' quick messages over the composer (iOS PromptBar's commands), the word and what it does; picking
 * one hands it to [onPick].
 */
@Composable
fun SlashPicker(commands: List<SlashCommand>, onPick: (SlashCommand) -> Unit) {
    val c = JTheme.colors
    Column(Modifier.fillMaxWidth().surface(RoundedCornerShape(JRadius.card), floating = true).padding(vertical = JSpace.xs)) {
        commands.forEach { cmd ->
            Row(
                Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).clickable { onPick(cmd) }.padding(horizontal = JSpace.l, vertical = JSpace.s),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
            ) {
                Text(cmd.word, style = JTheme.type.bodyStrong, maxLines = 1, modifier = Modifier.widthIn(min = JSize.box96),
                    color = if (cmd.word == SlashCommand.CLEAR) c.danger else c.label)
                Text(cmd.detail, style = JTheme.type.secondary, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * 语音输入 (iOS Dictation): the phone's own voice input screen (RecognizerIntent), so it works with whichever recognizer
 * the phone has (Xiaomi's, Google's…); what it heard goes to [onHeard]. Null when the phone has none.
 */
@Composable
fun rememberDictation(onHeard: (String) -> Unit, onError: (String) -> Unit): (() -> Unit)? {
    val context = LocalContext.current
    val intent = remember(context) {
        Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH)
            .putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            .putExtra(RecognizerIntent.EXTRA_LANGUAGE, "zh-CN")
            .putExtra(RecognizerIntent.EXTRA_PROMPT, "说吧，说完会放进输入框")
    }
    val available = remember(context) { runCatching { intent.resolveActivity(context.packageManager) != null }.getOrDefault(false) }
    val heard by rememberUpdatedState(onHeard)
    val failed by rememberUpdatedState(onError)
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { r ->
        r.data?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)?.firstOrNull()?.trim()?.takeIf { it.isNotEmpty() }?.let(heard)
    }
    if (!available) return null
    return { runCatching { launcher.launch(intent) }.onFailure { failed("这台手机上没有语音输入") } }
}
