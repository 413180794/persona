package persona.feature.knowledge

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import persona.core.contracts.api.KnowledgeEntry
import persona.core.contracts.api.KnowledgeUpdate
import persona.core.designsystem.*
import kotlin.math.roundToInt

/**
 * 打标 (iOS `KBTagEditor`): importance, relevance, tags and, for a project, its kind and status, set by hand. The hub
 * keeps them over its automatic tagging. Saving closes the sheet once the hub has them; a failure stays here.
 */
@Composable
internal fun KnowledgeTagEditor(entry: KnowledgeEntry, tagging: KnowledgeTagging, onSave: (KnowledgeUpdate) -> Unit, onDismiss: () -> Unit) {
    val start = remember(entry.key) { KnowledgeTagForm.of(entry) }
    var importance by rememberSaveable(entry.key) { mutableStateOf(start.importance) }
    var relevance by rememberSaveable(entry.key) { mutableIntStateOf(start.relevance) }
    var tags by rememberSaveable(entry.key) { mutableStateOf(start.tags) }
    var projectKind by rememberSaveable(entry.key) { mutableStateOf(start.projectKind) }
    var status by rememberSaveable(entry.key) { mutableStateOf(start.status) }
    val palette = PersonaTheme.palette
    JSheet(onDismiss = onDismiss, title = "打标 · ${entry.name}", dismissible = !tagging.busy, scrollable = true) {
        Column(Modifier.fillMaxWidth().imePadding()) {
            Label("重要级")
            Choices(KnowledgeTagForm.IMPORTANCE.map { it to it }, importance, enabled = !tagging.busy, status = ::importanceStatus) { importance = it }
            Label("与我相关 $relevance")
            JSlider(relevance.toFloat(), { relevance = it.roundToInt() }, valueRange = 0f..100f, step = 5f,
                enabled = !tagging.busy, contentDescription = "与我相关")
            Label("标签")
            JTextField(tags, { tags = it }, placeholder = "用空格分隔", singleLine = true, enabled = !tagging.busy)
            if (entry.isProject) {
                Label("类型")
                Choices(KnowledgeTagForm.PROJECT_KINDS, projectKind, enabled = !tagging.busy) { projectKind = it }
                Label("状态")
                Choices(KnowledgeTagForm.PROJECT_STATUSES, status, enabled = !tagging.busy) { status = it }
            }
            Spacer(Modifier.height(Tokens.Space.m))
            Text("手动打的标会锁住，hub 自动整理时不再覆盖。", style = PersonaTheme.type.caption, color = palette.ink3)
            tagging.error?.let {
                Spacer(Modifier.height(Tokens.Space.m))
                JNotice(it, status = Status.NeedsYou)
            }
            Spacer(Modifier.height(Tokens.Space.xl))
            JButton("保存", {
                onSave(KnowledgeTagForm(importance, relevance, tags, projectKind, status).update(entry.isProject))
            }, Modifier.fillMaxWidth(), loading = tagging.busy, loadingText = "正在保存…")
        }
    }
}

@Composable
private fun Label(text: String) {
    JSectionHeader(text)
}

/** A few choices all in sight (importance, a project's kind and status); labels stay on one line and scroll if need be. */
@Composable
private fun Choices(
    options: List<Pair<String, String>>,
    value: String,
    enabled: Boolean,
    status: (String) -> Status? = { null },
    onPick: (String) -> Unit,
) {
    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        options.forEach { (key, label) ->
            // Busy chips keep their click (ignored) so they keep their height while saving.
            JChip(label, selected = key == value, onClick = { if (enabled) onPick(key) }, status = status(key))
        }
    }
}
