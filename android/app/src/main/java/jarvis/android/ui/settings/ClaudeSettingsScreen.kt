package jarvis.android.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.UnfoldMore
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Hub
import jarvis.android.settings.ClaudeSetting
import jarvis.android.settings.SettingsPrefs
import jarvis.android.settings.claudeSettings
import jarvis.android.settings.hubMachineId
import jarvis.android.settings.setClaudeSetting
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/**
 * 设置 › Claude Code 设置 (iOS `ClaudeSettingsView`): the whitelisted `~/.claude/settings.json` options of one machine
 * (离开回顾、总是深度思考、思考强度、回复语言…), written to every online machine at once unless that is switched off.
 */
@Composable
fun ClaudeSettingsScreen(hub: Hub, onBack: () -> Unit) {
    val c = kitColors
    val ctx = LocalContext.current
    val machines by hub.machines.collectAsState()
    val online = machines.filter { it.online }
    var machineId by remember { mutableStateOf(SettingsPrefs.claudeMachine.orEmpty()) }
    var applyAll by remember { mutableStateOf(SettingsPrefs.claudeApplyAll) }
    var settings by remember { mutableStateOf<List<ClaudeSetting>>(emptyList()) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val drafts = remember { mutableStateMapOf<String, String>() }
    var pickMachine by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    // First visit: the hub's own machine; an offline pick falls back to the first online one.
    LaunchedEffect(machines) {
        val pick = machineId.takeIf { id -> online.any { it.id == id } } ?: hubMachineId(online)
        if (pick != null && pick != machineId) {
            machineId = pick
            SettingsPrefs.claudeMachine = pick
        }
    }

    suspend fun load() {
        if (machineId.isEmpty()) return
        loading = true
        try {
            settings = hub.api.claudeSettings(machineId)
            error = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.kitMessage
        } finally {
            loading = false
        }
    }
    LaunchedEffect(machineId) { drafts.clear(); settings = emptyList(); load() }

    fun set(key: String, value: JsonElement) {
        val targets = if (applyAll) online.map { it.id } else listOf(machineId)
        // Optimistic: the control moves at once; the hub's answer (or a reload on failure) corrects it.
        settings = settings.map { if (it.key == key) it.copy(value = value) else it }
        scope.launch {
            val failed = mutableListOf<String>()
            for (id in targets) {
                try {
                    val list = hub.api.setClaudeSetting(id, key, value)
                    if (id == machineId) settings = list
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    failed += "${machines.firstOrNull { it.id == id }?.displayName ?: id}：${e.kitMessage}"
                }
            }
            if (failed.isEmpty()) {
                error = null
                kitToast(ctx, if (targets.size > 1) "已应用到 ${targets.size} 台机器" else "已保存")
            } else {
                error = failed.joinToString("\n")
                load()
            }
        }
    }

    KitPage("Claude Code 设置", onBack, actions = {
        IconButton(onClick = { scope.launch { load() } }, enabled = !loading) { Icon(Icons.Default.Refresh, "刷新") }
    }) {
        item(key = "target") {
            KitGroup(
                Modifier.padding(top = 8.dp),
                footer = "改的是那台机器的 ~/.claude/settings.json，只动下面这几项；新开的 Claude 会话生效，正在跑的会话一般要重启才生效。",
            ) {
                row {
                    Box {
                        Row(
                            Modifier.fillMaxWidth().clickable(enabled = online.isNotEmpty(), role = Role.DropdownList) { pickMachine = true }
                                .heightIn(min = 48.dp).padding(horizontal = 16.dp, vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Text("机器", fontSize = 17.sp, color = c.label, modifier = Modifier.weight(1f), maxLines = 1)
                            Text(
                                machines.firstOrNull { it.id == machineId }?.displayName ?: if (online.isEmpty()) "没有在线的机器" else "",
                                fontSize = 17.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                            Icon(Icons.Default.UnfoldMore, null, tint = c.tertiaryLabel, modifier = Modifier.size(18.dp))
                        }
                        DropdownMenu(expanded = pickMachine, onDismissRequest = { pickMachine = false }) {
                            online.forEach { m ->
                                DropdownMenuItem(
                                    text = { Text(m.displayName, maxLines = 1) },
                                    trailingIcon = { if (m.id == machineId) Icon(Icons.Default.Check, null, tint = c.accent) },
                                    onClick = {
                                        pickMachine = false
                                        machineId = m.id
                                        SettingsPrefs.claudeMachine = m.id
                                    },
                                )
                            }
                        }
                    }
                }
                row {
                    KitSwitchRow("同时改所有在线机器", applyAll, onChange = { applyAll = it; SettingsPrefs.claudeApplyAll = it })
                }
            }
        }
        error?.let { msg ->
            item(key = "error") {
                KitGroup(Modifier.padding(top = 20.dp)) {
                    row { Text(msg, fontSize = 13.sp, color = c.danger, modifier = Modifier.padding(16.dp)) }
                }
            }
        }
        when {
            settings.isNotEmpty() -> item(key = "settings") {
                KitGroup(Modifier.padding(top = 20.dp)) {
                    settings.forEach { s -> row { SettingRow(s, drafts, ::set) } }
                }
            }
            loading -> item(key = "loading") {
                Box(Modifier.fillMaxWidth().padding(top = 60.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            }
            online.isEmpty() -> item(key = "none") {
                Text("没有在线的机器：机器连上 hub 后再来改。", fontSize = 15.sp, color = c.secondaryLabel,
                    modifier = Modifier.fillMaxWidth().padding(32.dp))
            }
        }
    }
}

@Composable
private fun SettingRow(s: ClaudeSetting, drafts: MutableMap<String, String>, set: (String, JsonElement) -> Unit) {
    val c = kitColors
    val focus = LocalFocusManager.current
    when (s.type) {
        "bool" -> Row(
            Modifier.fillMaxWidth().clickable(role = Role.Switch) { set(s.key, JsonPrimitive(!s.boolValue)) }
                .heightIn(min = 48.dp).padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SettingTitle(s, Modifier.weight(1f))
            Switch(checked = s.boolValue, onCheckedChange = null)
        }
        "enum" -> {
            var open by remember { mutableStateOf(false) }
            Box {
                Row(
                    Modifier.fillMaxWidth().clickable(role = Role.DropdownList) { open = true }
                        .heightIn(min = 48.dp).padding(horizontal = 16.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    SettingTitle(s, Modifier.weight(1f))
                    Text(s.stringValue, fontSize = 17.sp, color = c.secondaryLabel, maxLines = 1)
                    Icon(Icons.Default.UnfoldMore, null, tint = c.tertiaryLabel, modifier = Modifier.size(18.dp))
                }
                DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                    s.options.orEmpty().forEach { o ->
                        DropdownMenuItem(
                            text = { Text(o, maxLines = 1) },
                            trailingIcon = { if (o == s.stringValue) Icon(Icons.Default.Check, null, tint = c.accent) },
                            onClick = { open = false; if (o != s.stringValue) set(s.key, JsonPrimitive(o)) },
                        )
                    }
                }
            }
        }
        else -> Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            SettingTitle(s, Modifier.fillMaxWidth())
            val text = drafts[s.key] ?: s.stringValue
            fun commit() {
                focus.clearFocus()
                val v = drafts.remove(s.key)?.trim() ?: return
                // Emptied: back to Claude Code's default (the node removes the key).
                if (v != s.stringValue) set(s.key, if (v.isEmpty()) JsonNull else JsonPrimitive(v))
            }
            OutlinedTextField(
                value = text,
                onValueChange = { drafts[s.key] = it },
                placeholder = { Text(s.placeholder, color = c.tertiaryLabel) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { commit() }),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun SettingTitle(s: ClaudeSetting, modifier: Modifier) {
    val c = kitColors
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(s.title, fontSize = 17.sp, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
            if (s.isDefault) Text("默认", fontSize = 11.sp, color = c.tertiaryLabel, maxLines = 1)
        }
        if (s.detail.isNotEmpty()) Text(s.detail, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel)
    }
}
