package jarvis.feature.terminal

import android.content.ClipboardManager
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.*
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.*
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import jarvis.core.terminal.ModifierState
import jarvis.core.terminal.TerminalModifiers

@Composable
fun TerminalScreen(model: TerminalViewModel, onBack: () -> Unit, sessionTitle: String = model.key.sessionId) {
    val state by model.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val context = LocalContext.current
    var keyboard by remember { mutableStateOf(false) }
    var wrap by remember { mutableStateOf(true) }
    var canvas by remember { mutableStateOf<TerminalCanvasView?>(null) }
    var paste by remember { mutableStateOf<String?>(null) }
    val active = state.control == TerminalControl.Active && state.online
    val palette = JarvisTheme.palette
    LaunchedEffect(model, lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { model.run() } }
    LaunchedEffect(active) { if (!active) { keyboard = false; paste = null } }
    DisposableEffect(model) { onDispose { canvas?.keyboard(false) } }
    fun pasteClipboard() {
        val clip = context.getSystemService(ClipboardManager::class.java).primaryClip ?: return
        if (clip.itemCount == 0) return
        val text = clip.getItemAt(0).coerceToText(context).toString()
        if (text.contains('\n') || text.contains('\r')) paste = text else model.type(text)
    }
    // A terminal redraws all the time (every frame of the program, every wheel line): glass over it re-blurred its
    // bars and edges on each of those frames, on top of a slow link (2026-11-01). Its page is opaque in every style.
    CompositionLocalProvider(LocalGlassLevel provides GlassLevel.Solid) {
    JPage(sessionTitle, modifier = Modifier.imePadding(), topBar = {
        JTopBar(sessionTitle, subtitle = if (active) "手机 ${state.cols}×${state.rows}" else null, onBack = onBack, actions = {
            val watching = active || state.control == TerminalControl.Starting
            JChip(when { active -> "已接管"; watching -> "接管中"; else -> "只读" }, selected = active,
                onClick = if (watching) model::watch else if (state.online) ({ model.takeOver() }) else null)
            JIconButton(PhIcons.ArrowBendUpLeft, if (wrap) "自动换行，点按改为横滑" else "横向滚动，点按自动换行",
                { wrap = !wrap }, enabled = !active, selected = wrap)
        })
    }, bottomBar = {
        TerminalKeyBar(state.modifiers, { key, action, quick -> if (active) model.modifier(key, action, quick) }, model::press, keyboard,
            toggleKeyboard = { if (active) keyboard = !keyboard }, paste = { if (active) pasteClipboard() })
    }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            // The notice sits above the terminal, never over its text.
            state.note?.let { note -> JNotice(note, Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s), actionLabel = "重试", onAction = model::retry) }
            Box(Modifier.fillMaxWidth().weight(1f).background(palette.terminalBackground)) {
                AndroidView(factory = { ctx -> TerminalCanvasView(ctx, palette.terminalText.toArgb(), palette.terminalBackground.toArgb()).also { view ->
                    canvas = view
                    view.onGrid = model::grid; view.onText = model::type; view.onKey = model::press
                    view.onHardware = model::hardware; view.onMarked = model::marked; view.onWheel = model::wheel; view.onCell = model::click
                } }, modifier = Modifier.fillMaxSize().clipToBounds(), update = { view ->
                    view.render(state, wrap, palette.terminalText.toArgb(), palette.terminalBackground.toArgb()); view.keyboard(keyboard && active)
                }, onRelease = { it.keyboard(false); canvas = null })
                if (!state.loaded) JSpinner(Modifier.align(Alignment.Center))
            }
        }
    }
    }
    if (state.control == TerminalControl.Busy) JDialog(onDismiss = model::watch, title = "接管终端？",
        text = "电脑上另一个程序正在使用这个终端。接管后，它会断开连接，终端按手机尺寸显示。",
        confirm = "接管" to { model.takeOver(force = true) }, dismiss = "只读" to model::watch)
    paste?.let { text -> JDialog(onDismiss = { paste = null }, title = "粘贴多行内容？",
        content = { Text(text, style = JarvisTheme.type.code, color = JarvisTheme.palette.ink, maxLines = 8, overflow = TextOverflow.Ellipsis) },
        confirm = "粘贴" to { model.type(text); paste = null }) }
}

@Composable
private fun TerminalKeyBar(
    modifiers: Map<TerminalModifiers.Key, ModifierState>, modifierAction: (TerminalModifiers.Key, String, Boolean) -> Unit,
    press: (String) -> Unit, keyboard: Boolean, toggleKeyboard: () -> Unit, paste: () -> Unit,
) {
    val currentModifierAction by rememberUpdatedState(modifierAction)
    val currentToggle by rememberUpdatedState(toggleKeyboard)
    val currentPress by rememberUpdatedState(press)
    ComposerFrame(trailing = {
        JChip(if (keyboard) "收起" else "键盘", selected = keyboard, icon = PhIcons.Keyboard,
                modifier = Modifier.heightIn(min = 48.dp).semantics {
                    role = Role.Button; contentDescription = if (keyboard) "收起键盘，双击回车" else "打开键盘，双击回车"
                    onClick { currentToggle(); true }
                }.pointerInput(Unit) { detectTapGestures(onTap = { currentToggle() }, onDoubleTap = { currentPress("enter") }) })
    }, field = {
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            TerminalModifiers.Key.entries.forEach { key ->
                val value = modifiers[key] ?: ModifierState.OFF
                val label = when (key) { TerminalModifiers.Key.CTRL -> "Ctrl"; TerminalModifiers.Key.ALT -> "Alt"; TerminalModifiers.Key.SHIFT -> "⇧" }
                JChip(label, selected = value != ModifierState.OFF, modifier = Modifier.heightIn(min = 48.dp)
                    .semantics {
                        role = Role.Button; contentDescription = label; selected = value != ModifierState.OFF
                        stateDescription = when (value) { ModifierState.OFF -> "未启用"; ModifierState.ONCE -> "下一个键生效"; ModifierState.HELD -> "按住"; ModifierState.LOCKED -> "已锁定" }
                        onClick { currentModifierAction(key, "tap", false); true }
                    }.pointerInput(key) {
                        var lastTap = Long.MIN_VALUE / 2
                        awaitEachGesture {
                            val down = awaitFirstDown(requireUnconsumed = false); down.consume()
                            var canceled = false
                            val up = withTimeoutOrNull(viewConfiguration.longPressTimeoutMillis) {
                                waitForUpOrCancellation().also { canceled = it == null }
                            }
                            if (canceled) return@awaitEachGesture
                            if (up != null) {
                                currentModifierAction(key, "tap", up.uptimeMillis - lastTap <= viewConfiguration.doubleTapTimeoutMillis)
                                lastTap = up.uptimeMillis; up.consume()
                            } else {
                                currentModifierAction(key, "hold", false)
                                try { waitForUpOrCancellation()?.consume() } finally { currentModifierAction(key, "release", false) }
                                lastTap = Long.MIN_VALUE / 2
                            }
                        }
                    })
            }
            listOf("Esc" to "esc", "Tab" to "tab", "⌃C" to "ctrl+c", "←" to "left", "↓" to "down", "↑" to "up", "→" to "right", "↵" to "enter", "⌫" to "backspace").forEach { (label, key) ->
                JChip(label, onClick = { press(key) }, modifier = Modifier.heightIn(min = 48.dp))
            }
            JChip("粘贴", onClick = paste, modifier = Modifier.heightIn(min = 48.dp))
        }
    })
}
