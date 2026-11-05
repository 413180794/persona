package persona.platform.capsule

import androidx.compose.ui.res.stringResource
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.snapshots.SnapshotStateMap
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
    onSelectSection: (String) -> Unit,
    onOpen: (String?) -> Unit,
    onSend: suspend (String?, String) -> Unit,
    onAction: suspend (String?, String) -> Unit,
    drafts: SnapshotStateMap<Pair<String?, String?>, String>,
    extraContent: @Composable (CapsuleEntry?) -> Unit = {},
) {
    var revealed by remember(expanded) { mutableStateOf(false) }
    LaunchedEffect(expanded) { revealed = true }
    val progress by animateFloatAsState(if (revealed) 1f else 0f,
        if (PersonaTheme.reduceMotion) snap() else tween(PersonaTheme.motion.standard), label = "capsule-reveal")
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val selected = content.selected
    val status = (if (expanded) selected else content.urgent)?.state.status()
    val shape = if (expanded) PersonaTheme.shapes.panelShape else RoundedCornerShape(CapsuleTokens.compactRadius)
    val scope = rememberCoroutineScope()
    var operation by remember { mutableStateOf(false) }
    var failure by remember(content.sectionId, content.selectedId) { mutableStateOf<String?>(null) }
    val pending = content.busy || operation
    val draftKey = content.sectionId to content.selectedId
    val text = drafts[draftKey].orEmpty()
    val messageScroll = remember(draftKey) { androidx.compose.foundation.ScrollState(0) }
    val following = messageScroll.value >= messageScroll.maxValue - 1
    LaunchedEffect(draftKey, content.messages) {
        if (following) {
            withFrameNanos { }; withFrameNanos { }
            messageScroll.scrollTo(messageScroll.maxValue)
        }
    }
    val failed = stringResource(R.string.capsule_failure)
    val openDescription = stringResource(if (content.badgeCount > 0) R.string.capsule_open_new else R.string.capsule_open)
    fun run(block: suspend () -> Unit) {
        scope.launch {
            operation = true
            failure = null
            try { block() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { failure = error.message ?: failed }
            finally { operation = false }
        }
    }
    Column(
        Modifier.graphicsLayer {
            alpha = progress
            scaleX = CapsuleTokens.revealScale + (1f - CapsuleTokens.revealScale) * progress
            scaleY = scaleX
        }.shadow(CapsuleTokens.elevation, shape).clip(shape).background(palette.surfaceOpaque)
            .border(CapsuleTokens.border, if (expanded) palette.status(status).dot else palette.line, shape)
            .then(if (expanded) Modifier.width(CapsuleTokens.cardWidth).heightIn(max = CapsuleTokens.cardMaxHeight)
                else Modifier.size(CapsuleTokens.compactWidth, CapsuleTokens.orb))
            .testTag(if (expanded) "capsule.card" else "capsule.orb"),
    ) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = CapsuleTokens.orb)
                .then(if (!expanded) Modifier.clickable(onClick = onExpand).semantics {
                    contentDescription = openDescription
                } else Modifier)
                .padding(horizontal = if (expanded) CapsuleTokens.padding else CapsuleTokens.gap,
                    vertical = CapsuleTokens.smallGap),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap),
        ) {
            PersonaFace(when (status) {
                Status.NeedsYou -> PersonaState.NeedsYou
                Status.Working -> PersonaState.Working
                else -> PersonaState.Idle
            }, size = CapsuleTokens.face)
            if (expanded) {
                Column(Modifier.weight(1f)) {
                    Text(if (content.listing) content.section?.title.orEmpty() else selected?.title ?: content.section?.title ?: "Persona",
                        style = type.label, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (!content.listing && !selected?.summary.isNullOrBlank()) Text(selected?.summary.orEmpty(),
                        style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                JTextButton(stringResource(R.string.capsule_collapse), onCollapse, Modifier.testTag("capsule.collapse"))
            } else if (content.badgeCount > 0) {
                Box(Modifier.size(CapsuleTokens.badge).clip(CircleShape).background(palette.needsYou.dot)
                    .testTag("capsule.badge"))
            }
        }
        if (expanded) {
            Column(
                Modifier.weight(1f, fill = false).padding(horizontal = CapsuleTokens.padding).padding(bottom = CapsuleTokens.padding),
                verticalArrangement = Arrangement.spacedBy(CapsuleTokens.gap),
            ) {
                if (content.sections.isNotEmpty()) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.smallGap)) {
                    content.sections.forEach { section ->
                        JChip(section.title + if (section.count > 0) " ${section.count}" else "", Modifier.weight(1f).testTag("capsule.section.${section.id}"),
                            selected = content.sectionId == section.id, onClick = { onSelectSection(section.id) })
                    }
                }
                if (content.listing) {
                    Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()).testTag("capsule.list"),
                        verticalArrangement = Arrangement.spacedBy(CapsuleTokens.smallGap)) {
                        if (content.entries.isEmpty()) Text(stringResource(if (content.busy) R.string.capsule_loading else R.string.capsule_empty),
                            Modifier.padding(vertical = CapsuleTokens.padding), style = type.callout, color = palette.ink2)
                        content.entries.forEach { entry ->
                            JRow(entry.title, Modifier.fillMaxWidth().testTag("capsule.entry.${entry.id}"), subtitle = entry.summary,
                                subtitleLines = 1, leading = { JStatusDot(entry.state.status()) }, onClick = { run { onSelect(entry.id) } })
                        }
                        (content.error ?: failure)?.let { Text(it, style = type.caption, color = palette.danger.text) }
                    }
                } else {
                    if (content.section?.list == true) JTextButton(stringResource(R.string.capsule_back), { onSelect(null) }, Modifier.testTag("capsule.back"))
                    Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()),
                        verticalArrangement = Arrangement.spacedBy(CapsuleTokens.gap)) {
                        if (content.sections.isEmpty()) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap)) {
                            content.entries.forEach { entry ->
                                JChip(entry.title, selected = content.selectedId == entry.id, onClick = { onSelect(entry.id) })
                            }
                        }
                        if (content.messages.isNotEmpty()) {
                            Column(Modifier.fillMaxWidth().heightIn(max = CapsuleTokens.messageHeight).verticalScroll(messageScroll),
                                verticalArrangement = Arrangement.spacedBy(CapsuleTokens.gap)) {
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
                    if (content.canSend) JTextField(text, { drafts[draftKey] = it }, Modifier.fillMaxWidth().heightIn(max = CapsuleTokens.inputMaxHeight).testTag("capsule.input"),
                        placeholder = stringResource(if (selected?.state == CapsuleState.Working) R.string.capsule_placeholder_working else R.string.capsule_placeholder),
                        maxLines = 2, enabled = !pending)
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(CapsuleTokens.gap), verticalAlignment = Alignment.CenterVertically) {
                        JButton(stringResource(R.string.capsule_open_chat), { run { onOpen(content.selectedId) } }, Modifier.weight(1f), JButtonKind.Secondary, JButtonSize.Small)
                        if (content.canSend) JButton(stringResource(R.string.capsule_send), {
                            val sendingId = content.selectedId
                            val sendingDraft = draftKey
                            run { onSend(sendingId, text); drafts[sendingDraft] = "" }
                        }, Modifier.weight(1f).testTag("capsule.send"), size = JButtonSize.Small, enabled = text.isNotBlank() && !pending,
                            loading = pending)
                    }
                }
            }
        }
    }
}

private fun CapsuleState?.status(): Status = when (this) {
    CapsuleState.NeedsYou -> Status.NeedsYou
    CapsuleState.Working -> Status.Working
    CapsuleState.Done -> Status.Unread
    else -> Status.Idle
}
