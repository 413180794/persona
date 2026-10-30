package jarvis.android.ui

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.data.Interaction
import jarvis.android.data.Question
import jarvis.android.data.Resolution
import kotlinx.coroutines.launch

/**
 * A card for something the agent waits on (docs/protocol.md「Interaction」): pick answers, approve or send back a plan,
 * allow a tool, or press a terminal dialog's option. Every card can also hand the question back to the computer.
 */
@Composable
fun InteractionCard(hub: Hub, ix: Interaction, modifier: Modifier = Modifier) {
    val scope = rememberCoroutineScope()
    var busy by remember(ix.id) { mutableStateOf(false) }
    var error by remember(ix.id) { mutableStateOf<String?>(null) }

    fun resolve(r: Resolution) {
        busy = true; error = null
        scope.launch {
            try {
                hub.resolve(ix, r)
            } catch (e: HubException) {
                error = if (e.code == "prompt_changed") "电脑上的对话框变了，已按新的屏幕重新读取" else e.message
            } catch (e: Exception) {
                error = "没有送达：${e.message}"
            }
            busy = false
        }
    }

    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer),
        modifier = modifier.fillMaxWidth(),
    ) {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            when (ix.kind) {
                "question" -> QuestionBody(ix, busy, ::resolve)
                "plan" -> PlanBody(ix, busy, ::resolve)
                "permission" -> PermissionBody(ix, busy, ::resolve)
                "screen" -> ScreenBody(ix, busy, ::resolve)
                else -> Text(ix.headline)
            }
            error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            TextButton(onClick = { resolve(Resolution(dismiss = true)) }, enabled = !busy) { Text("交给电脑处理", maxLines = 1) }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun QuestionBody(ix: Interaction, busy: Boolean, resolve: (Resolution) -> Unit) {
    val qs = ix.questions.orEmpty()
    // question → chosen labels; free text per question overrides the options.
    val picked = remember(ix.id) { mutableStateMapOf<String, List<String>>() }
    val custom = remember(ix.id) { mutableStateMapOf<String, String>() }
    val single = qs.size == 1 && !qs[0].multiSelect

    qs.forEach { q: Question ->
        q.header?.takeIf { it.isNotBlank() }?.let { Text(it, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary) }
        Text(q.question, style = MaterialTheme.typography.titleSmall)
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            q.options.forEach { o ->
                val on = picked[q.question].orEmpty().contains(o.label)
                if (single) {
                    OutlinedButton(
                        onClick = { resolve(Resolution(answers = mapOf(q.question to o.label))) }, enabled = !busy,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Column(Modifier.fillMaxWidth()) {
                            Text(o.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            o.description?.takeIf { it.isNotBlank() }?.let {
                                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                } else {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(on, onCheckedChange = { c ->
                            val cur = picked[q.question].orEmpty()
                            picked[q.question] = when {
                                !q.multiSelect -> if (c) listOf(o.label) else emptyList()
                                c -> cur + o.label
                                else -> cur - o.label
                            }
                        }, enabled = !busy)
                        Column {
                            Text(o.label)
                            o.description?.takeIf { it.isNotBlank() }?.let {
                                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                }
            }
        }
        OutlinedTextField(
            custom[q.question].orEmpty(), { custom[q.question] = it }, placeholder = { Text("其他回答…") },
            modifier = Modifier.fillMaxWidth(), enabled = !busy,
        )
    }
    val answers = qs.associate { q ->
        q.question to (custom[q.question]?.takeIf { it.isNotBlank() } ?: picked[q.question].orEmpty().joinToString(", "))
    }
    val typed = qs.any { !custom[it.question].isNullOrBlank() }
    if (!single || typed) {
        Button(
            onClick = { resolve(Resolution(answers = answers)) },
            enabled = !busy && answers.values.all { it.isNotBlank() }, modifier = Modifier.fillMaxWidth(),
        ) { Text("提交回答", maxLines = 1) }
    }
}

@Composable
private fun PlanBody(ix: Interaction, busy: Boolean, resolve: (Resolution) -> Unit) {
    var feedback by remember(ix.id) { mutableStateOf("") }
    Text("计划", style = MaterialTheme.typography.titleSmall)
    Column(Modifier.heightIn(max = 360.dp).verticalScroll(rememberScrollState())) {
        Markdown(ix.plan.orEmpty())
    }
    OutlinedTextField(feedback, { feedback = it }, placeholder = { Text("要改的地方（打回时带上）") }, modifier = Modifier.fillMaxWidth(), enabled = !busy)
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.horizontalScroll(rememberScrollState())) {
        Button(onClick = { resolve(Resolution(approve = true, mode = "acceptEdits")) }, enabled = !busy) { Text("批准", maxLines = 1) }
        OutlinedButton(onClick = { resolve(Resolution(approve = true, mode = "bypassPermissions")) }, enabled = !busy) { Text("批准并全部放行", maxLines = 1) }
        OutlinedButton(onClick = { resolve(Resolution(approve = false, feedback = feedback)) }, enabled = !busy) { Text("打回", maxLines = 1) }
    }
}

@Composable
private fun PermissionBody(ix: Interaction, busy: Boolean, resolve: (Resolution) -> Unit) {
    Text("${ix.toolName ?: "工具"} 请求授权", style = MaterialTheme.typography.titleSmall)
    val detail = ix.summary?.takeIf { it.isNotBlank() } ?: ix.toolInput?.toString().orEmpty()
    Text(
        detail, fontFamily = FontFamily.Monospace, fontSize = 12.sp, maxLines = 12, overflow = TextOverflow.Ellipsis,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.horizontalScroll(rememberScrollState())) {
        Button(onClick = { resolve(Resolution(allow = true)) }, enabled = !busy) { Text("允许", maxLines = 1) }
        OutlinedButton(onClick = { resolve(Resolution(allow = true, always = true)) }, enabled = !busy) { Text("总是允许", maxLines = 1) }
        OutlinedButton(onClick = { resolve(Resolution(allow = false)) }, enabled = !busy) { Text("拒绝", maxLines = 1) }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ScreenBody(ix: Interaction, busy: Boolean, resolve: (Resolution) -> Unit) {
    var text by remember(ix.id) { mutableStateOf("") }
    val d = ix.dialog
    if (d != null && d.options.isNotEmpty()) {
        d.header?.takeIf { it.isNotBlank() }?.let { Text(it, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary) }
        Text(d.question, style = MaterialTheme.typography.titleSmall)
        d.detail?.takeIf { it.isNotBlank() }?.let {
            Text(it, fontFamily = FontFamily.Monospace, fontSize = 12.sp, maxLines = 10, overflow = TextOverflow.Ellipsis)
        }
        d.options.forEach { o ->
            if (o.kind == "type_something") {
                OutlinedTextField(text, { text = it }, placeholder = { Text(o.label) }, modifier = Modifier.fillMaxWidth(), enabled = !busy)
                OutlinedButton(onClick = { resolve(Resolution(option = o.key, text = text)) }, enabled = !busy && text.isNotBlank()) {
                    Text("发送", maxLines = 1)
                }
            } else {
                OutlinedButton(onClick = { resolve(Resolution(option = o.key)) }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
                    Text("${o.key}. ${o.label}", maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth())
                }
            }
        }
    } else {
        Text("终端里有对话框", style = MaterialTheme.typography.titleSmall)
        Text(
            ix.screen.orEmpty().lines().takeLast(16).joinToString("\n"), fontFamily = FontFamily.Monospace, fontSize = 11.sp,
            lineHeight = 14.sp, softWrap = false, modifier = Modifier.horizontalScroll(rememberScrollState()),
        )
        if (ix.secret == true) {
            OutlinedTextField(
                text, { text = it }, placeholder = { Text("密码") }, visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), modifier = Modifier.fillMaxWidth(),
            )
            Button(onClick = { resolve(Resolution(keys = listOf("enter"), text = text)) }, enabled = !busy && text.isNotEmpty()) { Text("输入", maxLines = 1) }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            listOf("1" to "1", "2" to "2", "3" to "3", "up" to "↑", "down" to "↓", "enter" to "回车", "esc" to "Esc").forEach { (k, l) ->
                AssistChip(onClick = { resolve(Resolution(keys = listOf(k))) }, label = { Text(l, maxLines = 1) }, enabled = !busy)
            }
        }
    }
}
