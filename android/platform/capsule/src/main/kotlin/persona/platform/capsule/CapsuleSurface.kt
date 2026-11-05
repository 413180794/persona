package persona.platform.capsule

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.designsystem.*

/** The native host renders this surface; [extraContent] is the declaration-only extension slot. */
@Composable
fun CapsuleSurface(
    content: CapsuleContent,
    expanded: Boolean,
    onExpand: () -> Unit,
    onCollapse: () -> Unit,
    onSelect: (String?) -> Unit,
    onOpen: (String?) -> Unit,
    onSend: suspend (String?, String) -> Unit,
    onAction: suspend (String?, String) -> Unit,
    extraContent: @Composable (CapsuleEntry?) -> Unit = {},
) {
    var revealed by remember(expanded) { mutableStateOf(false) }
    LaunchedEffect(expanded) { revealed = true }
    val progress by animateFloatAsState(if (revealed) 1f else 0f,
        if (PersonaTheme.reduceMotion) snap() else tween(PersonaTheme.motion.standard), label = "capsule-reveal")
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val selected = content.selected
    val top = if (expanded) selected else content.urgent
    val status = when (top?.state) {
        CapsuleState.NeedsYou -> Status.NeedsYou
        CapsuleState.Working -> Status.Working
        CapsuleState.Done -> Status.Unread
        else -> Status.Idle
    }
    val shape = if (expanded) PersonaTheme.shapes.panelShape else RoundedCornerShape(CapsuleTokens.orb)
    val scope = rememberCoroutineScope()
    var operation by remember { mutableStateOf(false) }
    var failure by remember(content.selectedId) { mutableStateOf<String?>(null) }
    val pending = content.busy || operation
    val drafts = remember { mutableStateMapOf<String?, String>() }
    val text = drafts[content.selectedId].orEmpty()
    val messageScroll = remember(content.selectedId) { androidx.compose.foundation.ScrollState(0) }
    val following = messageScroll.value >= messageScroll.maxValue - 1
    LaunchedEffect(content.selectedId, content.messages) {
        if (following) {
            withFrameNanos { }; withFrameNanos { }
            messageScroll.scrollTo(messageScroll.maxValue)
        }
    }
    fun run(block: suspend () -> Unit) {
        scope.launch {
            operation = true
            failure = null
            try { block() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { failure = error.message ?: "操作失败，请打开完整会话核对" }
            finally { operation = false }
        }
    }
    Column(
        Modifier.graphicsLayer {
            alpha = progress
            scaleX = CapsuleTokens.revealScale + (1f - CapsuleTokens.revealScale) * progress
            scaleY = scaleX
        }.shadow(CapsuleTokens.elevation, shape).clip(shape).background(palette.surfaceOpaque)
            .border(CapsuleTokens.border, palette.status(status).dot, shape)
            .then(if (expanded) Modifier.width(CapsuleTokens.cardWidth) else Modifier.widthIn(max = CapsuleTokens.summaryWidth))
            .testTag(if (expanded) "capsule.card" else "capsule.orb"),
    ) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = CapsuleTokens.orb)
                .then(if (!expanded) Modifier.clickable(onClick = onExpand).semantics { contentDescription = "打开 Persona 悬浮胶囊" } else Modifier)
                .padding(horizontal = if (expanded) CapsuleTokens.padding else CapsuleTokens.smallGap,
                    vertical = CapsuleTokens.smallGap),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap),
        ) {
            PersonaFace(when (status) {
                Status.NeedsYou -> PersonaState.NeedsYou
                Status.Working -> PersonaState.Working
                else -> PersonaState.Idle
            }, size = CapsuleTokens.face)
            if (expanded || top?.state != null && top.state != CapsuleState.Idle) {
                Column(Modifier.weight(1f)) {
                    Text(if (expanded) selected?.title ?: "Persona" else top?.title ?: "Persona",
                        style = type.label, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(if (expanded) selected?.summary ?: "随时接着聊" else top?.summary.orEmpty(),
                        style = type.caption, color = palette.status(status).text, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            if (!expanded && content.activeCount > 1) {
                JTag("${content.urgentCount}/${content.activeCount}", status = status, modifier = Modifier
                    .testTag("capsule.count")
                    .semantics { contentDescription = "同状态 ${content.urgentCount} 个，共 ${content.activeCount} 个任务" })
            }
            if (expanded) JTextButton("收起", onCollapse, Modifier.testTag("capsule.collapse"))
        }
        if (expanded) {
            Column(
                Modifier.weight(1f, fill = false).padding(horizontal = CapsuleTokens.padding).padding(bottom = CapsuleTokens.padding),
                verticalArrangement = Arrangement.spacedBy(CapsuleTokens.gap),
            ) {
                Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(CapsuleTokens.gap)) {
                    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap)) {
                        content.entries.sortedBy { it.state.ordinal }.forEach { entry ->
                            JChip(entry.title, selected = content.selectedId == entry.id, onClick = { onSelect(entry.id) })
                        }
                    }
                    if (content.messages.isNotEmpty()) {
                        Column(
                            Modifier.fillMaxWidth().heightIn(max = CapsuleTokens.messageHeight).verticalScroll(messageScroll),
                            verticalArrangement = Arrangement.spacedBy(CapsuleTokens.gap),
                        ) {
                            content.messages.takeLast(8).forEach { message ->
                                val bubble = if (message.fromUser) palette.accentSoft else palette.surfaceStrong
                                Text(message.text, Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape).background(bubble)
                                    .padding(CapsuleTokens.gap), style = type.callout, color = palette.ink)
                            }
                        }
                    }
                    extraContent(selected)
                    selected?.actions?.let { actions ->
                        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap)) {
                            actions.forEach { action ->
                                JButton(action.label, { run { onAction(content.selectedId, action.id) } }, kind = JButtonKind.Secondary,
                                    size = JButtonSize.Small, enabled = !pending)
                            }
                        }
                    }
                    (content.error ?: failure)?.let { Text(it, style = type.caption, color = palette.danger.text) }
                }
                if (content.canSend) {
                    JTextField(text, { drafts[content.selectedId] = it }, Modifier.fillMaxWidth().heightIn(max = CapsuleTokens.inputMaxHeight).testTag("capsule.input"),
                        placeholder = if (selected?.state == CapsuleState.Working) "补充或改要求…" else "想让 Persona 做什么？",
                        maxLines = 3, enabled = !pending)
                }
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap), verticalAlignment = Alignment.CenterVertically) {
                    JButton("打开会话", { onOpen(content.selectedId) }, Modifier.weight(1f), JButtonKind.Secondary, JButtonSize.Medium)
                    if (content.canSend) JButton("发送", {
                        val sendingId = content.selectedId
                        run { onSend(sendingId, text); drafts[sendingId] = "" }
                    }, Modifier.weight(1f).testTag("capsule.send"), size = JButtonSize.Medium, enabled = text.isNotBlank() && !pending,
                        loading = pending)
                }
            }
        }
    }
}
