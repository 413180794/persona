package jarvis.android.ui.chat

import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.JTheme
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Send
import androidx.compose.material.icons.rounded.AlternateEmail
import androidx.compose.material.icons.rounded.ContentCopy
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.FormatQuote
import androidx.compose.material.icons.rounded.Share
import androidx.compose.material.icons.rounded.TextFields
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import jarvis.android.data.*
import jarvis.android.ui.*
import jarvis.android.ui.media.*
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.LayoutCoordinates
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupPositionProvider
import androidx.compose.ui.window.PopupProperties

/**
 * WeChat-style message actions (iOS `MessageMenu`): long-press a message and a dark bar appears right above where you
 * pressed (below it near the top) — 复制 / 选择 / 引用 / 分享 / 交给 Jarvis / 作为上下文, plus 编辑 on your own messages.
 * A tap anywhere or scrolling the list closes it.
 */
@Stable
class MessageMenuState {
    data class Target(val id: String, val text: String, val isUser: Boolean, val at: Offset)

    var target by mutableStateOf<Target?>(null)
        private set
    var selecting by mutableStateOf<String?>(null)

    /** Set by the page: what 引用 / 编辑 / 交给 Jarvis / 作为上下文 do. */
    var quote: (String) -> Unit = {}
    var edit: (String) -> Unit = {}
    var askJarvis: ((String) -> Unit)? = null
    var asContext: ((String) -> Unit)? = null

    fun open(t: Target) { target = t }
    fun close() { target = null }
}

/** The pressable part of a message: a long press opens the menu at the finger (in window coordinates). */
@Composable
fun Modifier.messageLongPress(id: String, text: String, isUser: Boolean): Modifier {
    val menu = LocalChatEnv.current?.menu ?: return this
    val haptics = LocalHapticFeedback.current
    val coords = remember { arrayOfNulls<LayoutCoordinates>(1) }
    val selected = menu.target?.id == id
    return this
        .onGloballyPositioned { coords[0] = it }
        .alpha(if (selected) 0.75f else 1f)
        .pointerInput(id, text) {
            detectTapGestures(onLongPress = { p ->
                val at = coords[0]?.takeIf { it.isAttached }?.localToWindow(p) ?: p
                haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                menu.open(MessageMenuState.Target(id, text, isUser, at))
            })
        }
}

private class BarPosition(private val at: Offset, private val gap: Int, private val topLimit: Int) : PopupPositionProvider {
    override fun calculatePosition(anchorBounds: IntRect, windowSize: IntSize, layoutDirection: LayoutDirection, popupContentSize: IntSize): IntOffset {
        val margin = 8
        val x = (at.x.toInt() - popupContentSize.width / 2).coerceIn(margin, (windowSize.width - popupContentSize.width - margin).coerceAtLeast(margin))
        val above = at.y.toInt() - gap - popupContentSize.height
        val y = if (above > topLimit) above else at.y.toInt() + gap * 2
        return IntOffset(x, y)
    }
}

/** The bar's dark ink, the same in every style and appearance (iOS's menu bar). */
private val MenuInk = Color(0xFF2C2C2E)

/** The bar itself, over everything (a Popup at the press point). */
@Composable
fun MessageMenuHost(menu: MessageMenuState) {
    val t = menu.target ?: return
    val ctx = LocalContext.current
    val density = LocalDensity.current
    val gap = with(density) { 16.dp.roundToPx() }
    val top = with(density) { 140.dp.roundToPx() }
    Popup(
        popupPositionProvider = remember(t) { BarPosition(t.at, gap, top) },
        onDismissRequest = { menu.close() },
        properties = PopupProperties(focusable = true),
    ) {
        Row(
            Modifier.shadow(10.dp, RoundedCornerShape(JRadius.card)).background(MenuInk, RoundedCornerShape(JRadius.card))
                .horizontalScroll(rememberScrollState()).padding(horizontal = JSpace.sm).height(62.dp),
        ) {
            fun done() = menu.close()
            BarItem("复制", JIcons.copy) { MediaShare.copyText(ctx, t.text, "消息"); Toasts.show("已拷贝", ToastStyle.SUCCESS); done() }
            BarItem("选择", JIcons.select) { menu.selecting = t.text; done() }
            BarItem("引用", JIcons.quote) { menu.quote(t.text); done() }
            BarItem("分享", JIcons.share) { MediaShare.shareText(ctx, t.text); done() }
            menu.askJarvis?.let { ask -> BarItem("交给 Jarvis", JIcons.handToAI) { ask(t.text); done() } }
            menu.asContext?.let { add -> BarItem("作为上下文", JIcons.addContext) { add(t.text); done() } }
            if (t.isUser) BarItem("编辑", JIcons.edit) { menu.edit(t.text); done() }
        }
    }
}

@Composable
private fun BarItem(title: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    Column(
        Modifier.width(if (title.length > 3) 62.dp else 46.dp).height(62.dp).clickable(onClick = onClick).padding(top = JSpace.m),
        horizontalAlignment = androidx.compose.ui.Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(JSpace.xs),
    ) {
        // A fixed box: every label stays on one line whatever the icon's height.
        Box(Modifier.size(JSize.iconS), contentAlignment = androidx.compose.ui.Alignment.Center) { Icon(icon, null, tint = Color.White, modifier = Modifier.size(JSize.iconXS)) }
        Text(title, style = JTheme.type.label, color = Color.White, maxLines = 1, softWrap = false)
    }
}

/** 「选择」: the message as plain text where any part of it can be selected (iOS `SelectableTextSheet`). */
@Composable
fun SelectTextDialog(text: String, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    FullScreenPage(
        title = "选择文本",
        onBack = onDismiss,
        actions = {
            TextButton(onClick = { MediaShare.copyText(ctx, text, "消息"); Toasts.show("已拷贝", ToastStyle.SUCCESS); onDismiss() }) {
                Text("全部拷贝", style = JTheme.type.bodyStrong, color = JTheme.colors.accent, maxLines = 1)
            }
        },
    ) { pad ->
        SelectionContainer(Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState())) {
            Text(text, style = JTheme.type.body, lineHeight = 24.sp, modifier = Modifier.padding(horizontal = JSpace.l, vertical = JSpace.l))
        }
    }
}
