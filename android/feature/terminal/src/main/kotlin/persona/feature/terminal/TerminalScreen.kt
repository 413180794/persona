package persona.feature.terminal

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
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.semantics.*
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.terminal.ModifierState
import persona.core.terminal.TerminalModifiers

@Composable
fun TerminalScreen(model: TerminalViewModel, onBack: () -> Unit, sessionTitle: String = model.key.sessionId) {
    val state by model.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val context = LocalContext.current
    var keyboard by remember { mutableStateOf(false) }
    var wrap by remember { mutableStateOf(true) }
    var canvas by remember { mutableStateOf<TerminalCanvasView?>(null) }
    var paste by remember { mutableStateOf<String?>(null) }
    var selecting by remember { mutableStateOf<String?>(null) }
    var display by remember { mutableStateOf(false) }
    val active = state.driving
    // Keys work whenever the hub is reachable: straight into the stream while driving, through /keys while watching.
    val typing = state.online
    val palette = PersonaTheme.palette
    val feedback = LocalAppFeedback.current
    LaunchedEffect(model, lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { model.run() } }
    LaunchedEffect(model, feedback) { model.notices.collect { feedback?.notice(it) } }
    LaunchedEffect(typing) { if (!typing) { keyboard = false; paste = null } }
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
        JTopBar(sessionTitle, subtitle = terminalSubtitle(state), onBack = onBack, actions = {
            val watching = active || state.control == TerminalControl.Starting
            // Taking over needs the stream: a screen read once a second (an old hub or node) can only be watched.
            JChip(when { active -> "已接管"; watching -> "接管中"; else -> "只读" }, selected = active,
                onClick = if (watching) ({ model.watch() }) else if (state.online && state.streaming) ({ model.takeOver() }) else null)
            JIconButton(PhIcons.Copy, "选择文本", { selecting = model.screenText() }, enabled = state.loaded)
            JIconButton(PhIcons.SlidersHorizontal, "显示设置", { display = true })
        })
    }, bottomBar = {
        TerminalKeyBar(state.modifiers, typing, { key, action, quick -> if (typing) model.modifier(key, action, quick) }, model::press, keyboard,
            toggleKeyboard = { if (typing) keyboard = !keyboard }, paste = { if (typing) pasteClipboard() })
    }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            // OSC 8 links open where every link opens: the shell's in-app card (other schemes are confirmed first).
            val uriHandler = LocalUriHandler.current
            // The notice sits above the terminal, never over its text.
            state.note?.let { note -> JNotice(note, Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s), actionLabel = "重试", onAction = model::retry) }
            Box(Modifier.fillMaxWidth().weight(1f).background(palette.terminalBackground)) {
                AndroidView(factory = { ctx -> TerminalCanvasView(ctx, palette.terminalText.toArgb(), palette.terminalBackground.toArgb()).also { view ->
                    canvas = view
                    view.onGrid = model::grid; view.onText = model::type; view.onKey = model::press
                    view.onHardware = model::hardware; view.onMarked = model::marked; view.onWheel = model::wheel; view.onCell = model::click
                    view.onLink = { url -> runCatching { uriHandler.openUri(url) } }
                } }, modifier = Modifier.fillMaxSize().clipToBounds(), update = { view ->
                    view.font(state.fontSize)
                    view.render(state, wrap, palette.terminalText.toArgb(), palette.terminalBackground.toArgb()); view.keyboard(keyboard && typing)
                }, onRelease = { it.keyboard(false); canvas = null })
                if (!state.loaded) JSpinner(Modifier.align(Alignment.Center))
            }
        }
    }
    }
    if (state.control == TerminalControl.Busy) JDialog(onDismiss = { model.watch(remember = false) }, title = "接管终端？",
        text = "电脑上另一个程序正在使用这个终端。接管后，它会断开连接，终端按手机尺寸显示。",
        confirm = "接管" to { model.takeOver(force = true) }, dismiss = "只读" to { model.watch(remember = false) })
    paste?.let { text -> JDialog(onDismiss = { paste = null }, title = "粘贴多行内容？",
        content = { Text(text, style = PersonaTheme.type.code, color = PersonaTheme.palette.ink, maxLines = 8, overflow = TextOverflow.Ellipsis) },
        confirm = "粘贴" to { model.type(text); paste = null }) }
    selecting?.let { text -> TerminalSelectText(text) { selecting = null } }
    if (display) TerminalDisplaySheet(state.fontSize, model::font, wrap, { wrap = it }, wrapEnabled = !active, onDismiss = { display = false })
}

/** Under the title: the grid the phone drives, taking it over, the pane it mirrors, or the screen read once a second. */
private fun terminalSubtitle(state: TerminalUiState): String? = when {
    state.driving && state.cols > 0 -> "手机 ${state.cols}×${state.rows}"
    state.online && state.control == TerminalControl.Starting -> "正在接管…"
    !state.loaded || !state.online -> null
    state.streaming && state.cols > 0 -> "电脑 ${state.cols}×${state.rows}"
    !state.streaming -> "每秒读一次屏幕"
    else -> null
}

/** 显示: the font size (a step at a time, the sheet stays open) and folding what is only watched. */
@Composable
private fun TerminalDisplaySheet(
    fontSize: Float, onFont: (Boolean) -> Unit, wrap: Boolean, onWrap: (Boolean) -> Unit, wrapEnabled: Boolean, onDismiss: () -> Unit,
) {
    val palette = PersonaTheme.palette
    JSheet(onDismiss, title = "显示") {
        JRow("字号", subtitle = "接管时按新字号重新排版", trailing = {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                JButton("A−", { onFont(false) }, kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = fontSize > TerminalFontSize.MIN)
                Text(TerminalFontSize.label(fontSize), style = PersonaTheme.type.micro, color = palette.ink, textAlign = TextAlign.Center,
                    maxLines = 1, modifier = Modifier.widthIn(min = Tokens.Space.xxxl).semantics { contentDescription = "字号 ${TerminalFontSize.label(fontSize)}" })
                JButton("A＋", { onFont(true) }, kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = fontSize < TerminalFontSize.MAX)
            }
        })
        JSwitchRow("自动换行", wrap, onWrap, enabled = wrapEnabled,
            subtitle = if (wrapEnabled) "只看时按手机宽度折行，关掉后左右滑动" else "接管时按手机的宽度排版，不用折行")
    }
}

@Composable
private fun TerminalKeyBar(
    modifiers: Map<TerminalModifiers.Key, ModifierState>, enabled: Boolean, modifierAction: (TerminalModifiers.Key, String, Boolean) -> Unit,
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
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically) {
            TerminalModifiers.Key.entries.forEach { key ->
                val value = modifiers[key] ?: ModifierState.OFF
                val label = when (key) { TerminalModifiers.Key.CTRL -> "Ctrl"; TerminalModifiers.Key.ALT -> "Alt"; TerminalModifiers.Key.SHIFT -> "⇧" }
                val name = when (key) { TerminalModifiers.Key.CTRL -> "Ctrl"; TerminalModifiers.Key.ALT -> "Alt"; TerminalModifiers.Key.SHIFT -> "Shift" }
                JKeyCap(label, state = value.keyCap, enabled = enabled, modifier = Modifier
                    .semantics {
                        role = Role.Button; contentDescription = name; selected = value != ModifierState.OFF
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
            // ⇧Tab cycles Claude Code's modes (one key, not ⇧ then Tab).
            listOf("Esc" to "esc", "Tab" to "tab", "⇧Tab" to "shift+tab", "⌃C" to "ctrl+c", "←" to "left", "↓" to "down", "↑" to "up",
                "→" to "right", "↵" to "enter", "⌫" to "backspace").forEach { (label, key) ->
                JKeyCap(label, onClick = { press(key) }, enabled = enabled, contentDescription = keyNames[key])
            }
            JKeyCap("粘贴", onClick = paste, enabled = enabled)
        }
    })
}

private val ModifierState.keyCap: KeyCapState get() = when (this) {
    ModifierState.OFF -> KeyCapState.Off
    ModifierState.ONCE -> KeyCapState.Once
    ModifierState.HELD -> KeyCapState.Held
    ModifierState.LOCKED -> KeyCapState.Locked
}

/** What a screen reader says for the keys whose labels are symbols. */
private val keyNames = mapOf("shift+tab" to "Shift Tab", "ctrl+c" to "Control C", "left" to "左", "down" to "下", "up" to "上",
    "right" to "右", "enter" to "回车", "backspace" to "退格")
