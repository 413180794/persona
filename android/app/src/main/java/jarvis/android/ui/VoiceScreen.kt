package jarvis.android.ui

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.core.content.ContextCompat
import jarvis.android.data.Hub
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.RoundControl
import jarvis.android.ui.settings.KitChoiceRow
import jarvis.android.data.HubException
import jarvis.android.settings.modelPurpose
import jarvis.android.ui.settings.KitActionRow
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitLoadingRow
import jarvis.android.ui.settings.KitNoticeRow
import jarvis.android.ui.settings.ModelPurposeScreen
import jarvis.android.ui.settings.kitMessage
import kotlinx.coroutines.CancellationException
import jarvis.android.voice.Line
import jarvis.android.voice.Plugin
import jarvis.android.voice.Voice
import jarvis.android.voice.VoiceModels
import jarvis.android.voice.VoiceSession
import jarvis.android.voice.VoiceState
import jarvis.android.voice.voiceModels
import jarvis.android.voice.voices

/** What [VoiceScreen] draws instead of the real thing (screenshots): no microphone yet, or this conversation. */
sealed interface VoicePreview {
    data class NoMic(val denied: Boolean) : VoicePreview
    data class Live(val state: VoiceState) : VoicePreview
}

/**
 * 语音对话 (docs/plugins.md「语音」): full duplex with the hub's voice plugin. It listens all the time; talking over
 * an answer stops it. The voice (音色) is picked here and kept on this phone; changing it starts a new conversation.
 * The page: the header with 模型和音色, the orb and what is happening, the captions, then 静音 · 打断 · 挂断.
 */
@Composable
fun VoiceScreen(hub: Hub, plugin: Plugin, onBack: () -> Unit, preview: VoicePreview? = null) {
    val context = LocalContext.current
    // 模型配置 opens over the page (the voice purpose, the hub's); back closes it first.
    var configuring by remember { mutableStateOf(false) }
    BackHandler { if (configuring) configuring = false else onBack() }
    var granted by remember {
        mutableStateOf(
            if (preview != null) preview is VoicePreview.Live
            else ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED,
        )
    }
    var denied by remember { mutableStateOf((preview as? VoicePreview.NoMic)?.denied == true) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok -> granted = ok; denied = !ok }
    LaunchedEffect(Unit) { if (!granted && preview == null) ask.launch(Manifest.permission.RECORD_AUDIO) }

    // Global model selection belongs to the hub. Only the voice remains a preference on this phone.
    var modelId by remember(plugin.id) { mutableStateOf<String?>(null) }
    var configurationLoaded by remember(plugin.id) { mutableStateOf(false) }
    var configurationError by remember(plugin.id) { mutableStateOf<String?>(null) }
    var round by remember { mutableStateOf(0) }
    var models by remember { mutableStateOf<VoiceModels?>(null) }
    LaunchedEffect(plugin.id, round) {
        configurationLoaded = false
        configurationError = null
        try {
            models = hub.api.voiceModels(plugin.id)
            val purpose = try {
                hub.api.modelPurpose("plugin.${plugin.id}").purpose
            } catch (e: HubException) {
                if (e.status != 404) throw e
                null // older hubs still use their own default and the phone's legacy selection
            }
            val selected = purpose?.effective?.model ?: if (purpose == null) VoicePrefs.model(context, plugin.id) else null
            val status = purpose?.status?.takeIf { it.isNotBlank() && it != "未验证" }
            if (status != null) {
                configurationError = status
            } else if (selected != null && models!!.models.none { it.id == selected }) {
                configurationError = "配置的语音模型不在插件目录中，请在模型配置里重新选择。"
            } else {
                modelId = selected
                configurationLoaded = true
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            configurationError = e.kitMessage
        }
    }
    val model = modelId ?: models?.default
    var voiceId by remember { mutableStateOf(VoicePrefs.voice(context, plugin.id)) }
    var voices by remember { mutableStateOf<List<Voice>?>(null) }
    var defaultVoice by remember { mutableStateOf<String?>(null) }
    var voicesError by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(plugin.id, model) {
        voices = null; voicesError = null
        runCatching { hub.api.voices(plugin.id, model) }
            .onSuccess { r ->
                voices = r.voices; defaultVoice = r.default
                if (voiceId != null && r.voices.none { it.id == voiceId }) voiceId = null
            }
            .onFailure { voicesError = it.message }
    }
    var menu by remember { mutableStateOf(false) }
    var picking by remember { mutableStateOf<Pick?>(null) }
    val voiceName = voices?.firstOrNull { it.id == (voiceId ?: defaultVoice) }?.name
    val modelName = models?.models?.firstOrNull { it.id == model }?.label ?: model

    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
            PageHeader(plugin.name.ifEmpty { "语音" }, onBack, subtitle = listOfNotNull(modelName, voiceName).joinToString(DOT_SEP)) {
                Box {
                    CircleButton(JIcons.options, "模型和音色", { menu = true })
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(
                            text = { Text("模型配置", maxLines = 1) }, leadingIcon = { Icon(JIcons.model, null) },
                            onClick = { menu = false; configuring = true },
                        )
                        DropdownMenuItem(
                            text = { Text("音色", maxLines = 1) }, leadingIcon = { Icon(JIcons.voice, null) },
                            onClick = { menu = false; picking = Pick.VOICE },
                        )
                    }
                }
            }
            Box(Modifier.weight(1f).fillMaxWidth()) {
                when {
                    !granted -> NoMic(denied, onAsk = { ask.launch(Manifest.permission.RECORD_AUDIO) })
                    preview is VoicePreview.Live -> ConversationView(preview.state, onMute = {}, onInterrupt = {}, onRestart = {}, onHangUp = onBack)
                    // The hub's voice model is unusable (not configured, gone from the plugin): say so, offer to fix it.
                    configurationError != null -> Column(Modifier.fillMaxWidth()) {
                        KitGroup {
                            row { KitNoticeRow("语音模型不可用", configurationError) }
                            row { KitActionRow("模型配置", { configuring = true }) }
                            row { KitActionRow("重试", { round++ }) }
                        }
                    }
                    !configurationLoaded -> KitLoadingRow()
                    // Changing either starts a new conversation (they are fixed when a session starts).
                    else -> key(round, modelId, voiceId) {
                        Conversation(context, hub, plugin, voiceId, modelId, onRestart = { configurationLoaded = false; round++ }, onHangUp = onBack)
                    }
                }
            }
        }
        when (picking) {
            Pick.VOICE -> PickSheet(
                title = "音色", onDismiss = { picking = null }, error = voicesError?.let { "音色列表没拿到：$it" },
                rows = voices?.map { v -> PickRow(v.id, v.name, voiceDetail(v, v.id == defaultVoice)) },
                chosen = voiceId ?: defaultVoice,
                onPick = { id -> voiceId = id; VoicePrefs.setVoice(context, plugin.id, id) },
            )
            null -> {}
        }
        if (configuring) {
            ModelPurposeScreen(hub, "plugin.${plugin.id}", onBack = {
                configuring = false
                if (!configurationLoaded) round++
            })
        }
    }
}

private enum class Pick { VOICE }

private data class PickRow(val id: String, val name: String, val detail: String?)

/** 音色: the choices as one group, the current one checked; a tap picks and closes. */
@Composable
private fun PickSheet(title: String, onDismiss: () -> Unit, error: String?, rows: List<PickRow>?, chosen: String?, onPick: (String) -> Unit) {
    JSheet(onDismiss, title = title, onClose = onDismiss, halfFirst = true) {
        when {
            rows == null && error == null -> Box(Modifier.fillMaxWidth().padding(JSpace.xxl), Alignment.Center) {
                CircularProgressIndicator(Modifier.size(JSize.wellXS), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText)
            }
            rows == null -> ErrorLine(error.orEmpty(), Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.xs, vertical = JSpace.l))
            else -> LazyColumn(contentPadding = PaddingValues(top = JSpace.s)) {
                itemsIndexed(rows, key = { _, r -> r.id }) { i, r ->
                    val isChosen = r.id == chosen
                    CardSlice(first = i == 0, last = i == rows.lastIndex, dividerStart = JSpace.l) {
                        KitChoiceRow(r.name, isChosen, onClick = { onDismiss(); if (!isChosen) onPick(r.id) }, detail = r.detail)
                    }
                }
            }
        }
    }
}

private fun voiceDetail(v: Voice, isDefault: Boolean): String? = listOfNotNull(
    when (v.gender) { "female" -> "女声"; "male" -> "男声"; else -> null },
    v.languages.mapNotNull { LANGS[it] }.joinToString("、").ifEmpty { null },
    "默认".takeIf { isDefault },
).joinToString(DOT_SEP).ifEmpty { null }

private val LANGS = mapOf("zh" to "中文", "en" to "英文", "ja" to "日语", "ko" to "韩语", "es" to "西班牙语", "fr" to "法语")

@Composable
private fun NoMic(denied: Boolean, onAsk: () -> Unit) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        EmptyState(
            JIcons.micOff,
            if (denied) "没有麦克风权限" else "语音对话要用麦克风",
            if (denied) "语音对话用不了。可以在系统设置里给 Jarvis 打开麦克风。" else "只在这一页打开时听，离开就结束，不在后台录音。",
            action = "允许使用麦克风" to onAsk,
        )
    }
}

@Composable
private fun Conversation(context: Context, hub: Hub, plugin: Plugin, voice: String?, model: String?, onRestart: () -> Unit, onHangUp: () -> Unit) {
    val session = remember { VoiceSession(context.applicationContext, hub.api, plugin.id, voice, model) }
    val view = androidx.compose.ui.platform.LocalView.current
    DisposableEffect(session) {
        session.start()
        view.keepScreenOn = true // a conversation, not a screen to read: the screen going off would end it
        onDispose { session.stop(); view.keepScreenOn = false }
    }
    val s by session.state.collectAsState()
    ConversationView(s, onMute = { session.setMuted(!s.muted) }, onInterrupt = session::interrupt, onRestart = onRestart, onHangUp = onHangUp)
}

/** The live page: the orb, what is happening (and what went wrong), the captions, then the controls. */
@Composable
private fun ConversationView(s: VoiceState, onMute: () -> Unit, onInterrupt: () -> Unit, onRestart: () -> Unit, onHangUp: () -> Unit) {
    val list = rememberLazyListState()
    LaunchedEffect(s.lines.size, s.lines.lastOrNull()?.text) { if (s.lines.isNotEmpty()) list.animateScrollToItem(s.lines.size - 1) }
    val c = JTheme.colors

    Column(Modifier.fillMaxSize().navigationBarsPadding()) {
        Box(Modifier.fillMaxWidth().weight(0.42f), contentAlignment = Alignment.Center) {
            Orb(s)
        }
        Text(
            statusText(s), style = JTheme.type.secondaryMedium, color = c.secondaryText,
            textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.xl),
        )
        s.error?.let { ErrorLine(it, Modifier.fillMaxWidth().padding(horizontal = JSpace.xl).padding(top = JSpace.s), centered = true) }
        LazyColumn(
            state = list, modifier = Modifier.weight(0.58f).fillMaxWidth(),
            contentPadding = PaddingValues(horizontal = JSpace.page, vertical = JSpace.l), verticalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            items(s.lines, key = { it.id }) { Subtitle(it) }
        }
        Row(
            Modifier.fillMaxWidth().padding(top = JSpace.s, bottom = JSpace.xl),
            horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.Top,
        ) {
            if (s.phase == VoiceState.Phase.ENDED) {
                RoundControl(JIcons.dictate, "重新开始", onRestart, fill = c.accent, tint = Color.White)
            } else {
                // Muted, the button turns to ink like a call's: what is on stands out.
                RoundControl(
                    if (s.muted) JIcons.micOff else JIcons.dictate, if (s.muted) "取消静音" else "静音", onMute,
                    fill = if (s.muted) JTheme.shell.ink else c.fillChip, tint = if (s.muted) JTheme.shell.inkText else c.label,
                )
                RoundControl(JIcons.interrupt, "打断", onInterrupt, enabled = s.speaking)
            }
            RoundControl(JIcons.hangUp, "挂断", onHangUp, fill = c.danger, tint = Color.White)
        }
    }
}

private fun statusText(s: VoiceState): String = when {
    s.phase == VoiceState.Phase.CONNECTING -> "正在连接…"
    s.phase == VoiceState.Phase.ENDED -> "对话已结束"
    s.tool != null -> "${s.tool}…"
    s.speaking -> "正在说，直接开口就能打断"
    s.muted -> "已静音"
    else -> "在听"
}

// The orb's own rhythm: a quick beat while it speaks, a slow breath while it listens, the voice's level followed closely.
private const val PULSE_SPEAKING_MS = 420
private const val PULSE_LISTENING_MS = 1600
private const val LEVEL_MS = 90

@Composable
private fun Orb(s: VoiceState) {
    val pulse by rememberInfiniteTransition(label = "orb").animateFloat(
        0.96f, 1.04f, infiniteRepeatable(tween(if (s.speaking) PULSE_SPEAKING_MS else PULSE_LISTENING_MS), RepeatMode.Reverse), label = "pulse",
    )
    val live = s.phase == VoiceState.Phase.LIVE
    val target = when {
        !live -> 0.85f
        s.speaking -> pulse * 1.08f
        s.muted -> 0.9f
        else -> 1f + s.level * 0.35f
    }
    val scale by animateFloatAsState(target, tween(LEVEL_MS), label = "scale")
    val c = JTheme.colors
    // Listening in the accent; speaking, the AI's color comes in.
    val colors = if (s.speaking) listOf(c.ai, c.accent) else listOf(c.accent, c.userBubble)
    Box(contentAlignment = Alignment.Center) {
        Box(Modifier.size(JSize.box200).scale(scale * 1.12f).clip(CircleShape).background(c.accent.copy(alpha = if (live) 0.12f else 0.06f)))
        Box(Modifier.size(JSize.box160).scale(scale).clip(CircleShape).background(Brush.linearGradient(colors)))
        if (s.phase == VoiceState.Phase.CONNECTING) CircularProgressIndicator(color = Color.White)
    }
}

/** One caption: yours in your bubble on the right, Jarvis' as plain words (the assistant's look); faint until final. */
@Composable
private fun Subtitle(l: Line) {
    val c = JTheme.colors
    val faint = if (l.final) 1f else 0.6f
    if (l.user) {
        Row(Modifier.fillMaxWidth().padding(start = JSize.avatarXL), horizontalArrangement = Arrangement.End) {
            Text(
                l.text, style = JTheme.type.body, color = c.userBubbleText.copy(alpha = faint),
                modifier = Modifier.widthIn(max = JSize.box300).background(c.userBubble, RoundedCornerShape(JRadius.bubble))
                    .padding(horizontal = JSpace.l, vertical = JSpace.m),
            )
        }
    } else {
        Text(l.text, style = JTheme.type.body, color = c.label.copy(alpha = faint), modifier = Modifier.fillMaxWidth().padding(end = JSize.avatarXL))
    }
}

/** Voice is a local preference; the legacy model is read only when connected to an older hub. */
object VoicePrefs {
    private fun sp(ctx: Context) = ctx.getSharedPreferences("voice", Context.MODE_PRIVATE)
    fun voice(ctx: Context, plugin: String): String? = sp(ctx).getString("voice.$plugin", null)
    fun setVoice(ctx: Context, plugin: String, id: String) = sp(ctx).edit().putString("voice.$plugin", id).apply()
    fun model(ctx: Context, plugin: String): String? = sp(ctx).getString("model.$plugin", null)
}
