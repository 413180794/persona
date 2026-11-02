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
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CallEnd
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.filled.RecordVoiceOver
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material.icons.filled.StopCircle
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import jarvis.android.data.Hub
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
import jarvis.android.voice.VoiceSession
import jarvis.android.voice.VoiceModels
import jarvis.android.voice.VoiceState
import jarvis.android.voice.voiceModels
import jarvis.android.voice.voices

/**
 * 语音对话 (docs/plugins.md「语音」): full duplex with the hub's voice plugin. It listens all the time; talking over
 * an answer stops it. The voice (音色) is picked here and kept on this phone; changing it starts a new conversation.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VoiceScreen(hub: Hub, plugin: Plugin, onBack: () -> Unit) {
    val context = LocalContext.current
    var configuring by remember { mutableStateOf(false) }
    BackHandler { if (configuring) configuring = false else onBack() }
    var granted by remember { mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) }
    var denied by remember { mutableStateOf(false) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok -> granted = ok; denied = !ok }
    LaunchedEffect(Unit) { if (!granted) ask.launch(Manifest.permission.RECORD_AUDIO) }

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

    Box {
        Scaffold(
            topBar = {
                TopAppBar(
                    title = {
                        Column {
                            Text(plugin.name.ifEmpty { "语音" }, maxLines = 1)
                            val sub = listOfNotNull(modelName, voiceName).joinToString(" · ")
                            if (sub.isNotEmpty()) {
                                Text(sub, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                        }
                    },
                    navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                    actions = {
                        Box {
                            IconButton(onClick = { menu = true }) { Icon(Icons.Default.Tune, "模型和音色") }
                            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                                DropdownMenuItem(
                                    text = { Text("模型配置", maxLines = 1) }, leadingIcon = { Icon(Icons.Default.Memory, null) },
                                    onClick = { menu = false; configuring = true },
                                )
                                DropdownMenuItem(
                                    text = { Text("音色", maxLines = 1) }, leadingIcon = { Icon(Icons.Default.RecordVoiceOver, null) },
                                    onClick = { menu = false; picking = Pick.VOICE },
                                )
                            }
                        }
                    },
                )
            },
        ) { pad ->
            Box(Modifier.padding(pad).fillMaxSize()) {
                when {
                    !granted -> NoMic(denied, onAsk = { ask.launch(Manifest.permission.RECORD_AUDIO) })
                    configurationError != null -> KitGroup {
                        row { KitNoticeRow("语音模型不可用", configurationError) }
                        row { KitActionRow("模型配置", { configuring = true }) }
                        row { KitActionRow("重试", { round++ }) }
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PickSheet(title: String, onDismiss: () -> Unit, error: String?, rows: List<PickRow>?, chosen: String?, onPick: (String) -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Text(title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(start = 24.dp, bottom = 8.dp))
        when {
            rows == null && error == null -> Box(Modifier.fillMaxWidth().padding(32.dp), Alignment.Center) { CircularProgressIndicator() }
            rows == null -> Text(error.orEmpty(), color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(24.dp))
            else -> LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
                items(rows, key = { it.id }) { r ->
                    val isChosen = r.id == chosen
                    ListItem(
                        headlineContent = { Text(r.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        supportingContent = r.detail?.let { { Text(it, maxLines = 1, overflow = TextOverflow.Ellipsis) } },
                        trailingContent = if (isChosen) ({ Icon(Icons.Default.Check, "已选") }) else null,
                        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
                        modifier = Modifier.clickable { onDismiss(); if (!isChosen) onPick(r.id) },
                    )
                }
            }
        }
    }
}

private fun voiceDetail(v: Voice, isDefault: Boolean): String? = listOfNotNull(
    when (v.gender) { "female" -> "女声"; "male" -> "男声"; else -> null },
    v.languages.mapNotNull { LANGS[it] }.joinToString("、").ifEmpty { null },
    "默认".takeIf { isDefault },
).joinToString(" · ").ifEmpty { null }

private val LANGS = mapOf("zh" to "中文", "en" to "英文", "ja" to "日语", "ko" to "韩语", "es" to "西班牙语", "fr" to "法语")

@Composable
private fun NoMic(denied: Boolean, onAsk: () -> Unit) {
    Column(Modifier.fillMaxSize().padding(32.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        Icon(Icons.Default.MicOff, null, Modifier.size(48.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.height(16.dp))
        Text(
            if (denied) "没有麦克风权限，语音对话用不了。可以在系统设置里给 Jarvis 打开麦克风。" else "语音对话要用麦克风。",
            textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(16.dp))
        TextButton(onClick = onAsk) { Text("允许使用麦克风", maxLines = 1) }
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
    val list = rememberLazyListState()
    LaunchedEffect(s.lines.size, s.lines.lastOrNull()?.text) { if (s.lines.isNotEmpty()) list.animateScrollToItem(s.lines.size - 1) }

    Column(Modifier.fillMaxSize().navigationBarsPadding()) {
        Box(Modifier.fillMaxWidth().weight(0.42f), contentAlignment = Alignment.Center) {
            Orb(s)
        }
        Text(
            statusText(s), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp),
        )
        s.error?.let {
            Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 4.dp))
        }
        LazyColumn(
            state = list, modifier = Modifier.weight(0.58f).fillMaxWidth(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(s.lines, key = { it.id }) { Subtitle(it) }
        }
        Row(
            Modifier.fillMaxWidth().padding(vertical = 20.dp), horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.CenterVertically,
        ) {
            if (s.phase == VoiceState.Phase.ENDED) {
                FilledTonalIconButton(onClick = onRestart, modifier = Modifier.size(64.dp)) { Icon(Icons.Default.Mic, "重新开始") }
            } else {
                FilledTonalIconButton(onClick = { session.setMuted(!s.muted) }, modifier = Modifier.size(64.dp)) {
                    Icon(if (s.muted) Icons.Default.MicOff else Icons.Default.Mic, if (s.muted) "取消静音" else "静音")
                }
                FilledTonalIconButton(onClick = session::interrupt, enabled = s.speaking, modifier = Modifier.size(64.dp)) {
                    Icon(Icons.Default.StopCircle, "打断")
                }
            }
            FilledIconButton(
                onClick = onHangUp, modifier = Modifier.size(64.dp),
                colors = IconButtonDefaults.filledIconButtonColors(containerColor = MaterialTheme.colorScheme.error, contentColor = MaterialTheme.colorScheme.onError),
            ) { Icon(Icons.Default.CallEnd, "挂断") }
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

@Composable
private fun Orb(s: VoiceState) {
    val pulse by rememberInfiniteTransition(label = "orb").animateFloat(
        0.96f, 1.04f, infiniteRepeatable(tween(if (s.speaking) 420 else 1600), RepeatMode.Reverse), label = "pulse",
    )
    val live = s.phase == VoiceState.Phase.LIVE
    val target = when {
        !live -> 0.85f
        s.speaking -> pulse * 1.08f
        s.muted -> 0.9f
        else -> 1f + s.level * 0.35f
    }
    val scale by animateFloatAsState(target, tween(90), label = "scale")
    val c = MaterialTheme.colorScheme
    val colors = if (s.speaking) listOf(c.tertiary, c.primary) else listOf(c.primary, c.primaryContainer)
    Box(contentAlignment = Alignment.Center) {
        Box(Modifier.size(200.dp).scale(scale * 1.12f).clip(CircleShape).background(c.primary.copy(alpha = if (live) 0.12f else 0.06f)))
        Box(Modifier.size(160.dp).scale(scale).clip(CircleShape).background(Brush.linearGradient(colors)))
        if (s.phase == VoiceState.Phase.CONNECTING) CircularProgressIndicator(color = c.onPrimary)
    }
}

@Composable
private fun Subtitle(l: Line) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = if (l.user) Arrangement.End else Arrangement.Start) {
        Surface(
            shape = RoundedCornerShape(16.dp),
            color = if (l.user) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant,
            modifier = Modifier.widthIn(max = 300.dp),
        ) {
            Text(
                l.text, style = MaterialTheme.typography.bodyMedium,
                color = (if (l.user) MaterialTheme.colorScheme.onPrimaryContainer else MaterialTheme.colorScheme.onSurfaceVariant)
                    .copy(alpha = if (l.final) 1f else 0.7f),
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
    }
}

/** Voice is a local preference; the legacy model is read only when connected to an older hub. */
object VoicePrefs {
    private fun sp(ctx: Context) = ctx.getSharedPreferences("voice", Context.MODE_PRIVATE)
    fun voice(ctx: Context, plugin: String): String? = sp(ctx).getString("voice.$plugin", null)
    fun setVoice(ctx: Context, plugin: String, id: String) = sp(ctx).edit().putString("voice.$plugin", id).apply()
    fun model(ctx: Context, plugin: String): String? = sp(ctx).getString("model.$plugin", null)
}
