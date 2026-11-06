package persona.feature.agents

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.todoHandOffPrompt
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

enum class HandOffMode(@androidx.annotation.StringRes val title: Int) {
    New(R.string.agents_handoff_new), Existing(R.string.agents_handoff_existing)
}

@Composable
fun TodoHandOffSheet(
    todo: Todo,
    machines: List<Machine>,
    sessions: List<Session>,
    onDismiss: () -> Unit,
    onHandOffNew: (machineId: String, cwd: String, launcher: String, permissionMode: String?, prompt: String) -> Unit,
    onHandOffExisting: (target: SessionKey, prompt: String) -> Unit,
    /** The caller's start is under way; it ends on every outcome but an opened chat. */
    starting: Boolean = false,
    /** Why the last tap didn't open a chat (a rejected or unconfirmed start). */
    error: String? = null,
) {
    var mode by remember { mutableStateOf(HandOffMode.New) }
    var instruction by remember { mutableStateOf("") }

    val onlineMachines = remember(machines) { machines.filter { it.online }.ifEmpty { machines } }
    var selectedMachine by remember(onlineMachines) { mutableStateOf(onlineMachines.firstOrNull()) }
    var cwd by remember { mutableStateOf("~") }
    var launcher by remember { mutableStateOf("claude") }
    var permissionMode by remember { mutableStateOf("default") }
    var showMachineMenu by remember { mutableStateOf(false) }
    var showLauncherMenu by remember { mutableStateOf(false) }
    var showModeMenu by remember { mutableStateOf(false) }

    val runningAgents = remember(sessions) {
        sessions.filter { !it.assistant && it.isLive }
            .sortedWith(compareByDescending<Session> { it.updated }.thenBy { it.sessionId })
    }
    var selectedTarget by remember(runningAgents) { mutableStateOf(runningAgents.firstOrNull()?.key) }

    JSheet(onDismiss = onDismiss, title = stringResource(R.string.agents_todo_hand_to_agent)) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                HandOffMode.entries.forEach { m ->
                    JChip(
                        stringResource(m.title),
                        selected = mode == m,
                        onClick = { mode = m },
                        modifier = Modifier.weight(1f)
                    )
                }
            }

            if (mode == HandOffMode.New) {
                JSectionHeader(stringResource(R.string.agents_handoff_new_session))
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Box {
                        JGroupItem(GroupPosition.of(0, 4)) {
                            JRow(
                                stringResource(R.string.agents_handoff_computer),
                                subtitle = selectedMachine?.let { if (it.online) it.displayName else stringResource(R.string.agents_handoff_machine_offline, it.displayName) }
                                    ?: stringResource(R.string.agents_handoff_pick_computer),
                                leadingIcon = PhIcons.Desktop,
                                chevron = true,
                                onClick = { showMachineMenu = true }
                            )
                        }
                        DropdownMenu(showMachineMenu, onDismissRequest = { showMachineMenu = false }) {
                            machines.forEach { m ->
                                DropdownMenuItem(
                                    text = { Text(if (m.online) m.displayName else stringResource(R.string.agents_handoff_machine_offline, m.displayName)) },
                                    onClick = {
                                        selectedMachine = m
                                        showMachineMenu = false
                                    }
                                )
                            }
                        }
                    }

                    JGroupItem(GroupPosition.of(1, 4)) {
                        JTextField(cwd, { cwd = it }, label = stringResource(R.string.agents_handoff_directory), singleLine = true)
                    }

                    Box {
                        JGroupItem(GroupPosition.of(2, 4)) {
                            JRow(
                                stringResource(R.string.agents_new_launcher),
                                subtitle = launcher,
                                leadingIcon = PhIcons.RocketLaunch,
                                chevron = true,
                                onClick = { showLauncherMenu = true }
                            )
                        }
                        DropdownMenu(showLauncherMenu, onDismissRequest = { showLauncherMenu = false }) {
                            listOf("claude", "codex").forEach { l ->
                                DropdownMenuItem(
                                    text = { Text(l) },
                                    onClick = {
                                        launcher = l
                                        showLauncherMenu = false
                                    }
                                )
                            }
                        }
                    }

                    Box {
                        JGroupItem(GroupPosition.of(3, 4)) {
                            JRow(
                                stringResource(R.string.agents_new_permission_mode),
                                subtitle = stringResource(if (permissionMode == "bypassPermissions") R.string.agents_handoff_mode_bypass else R.string.agents_handoff_mode_default),
                                leadingIcon = PhIcons.ShieldCheck,
                                chevron = true,
                                onClick = { showModeMenu = true }
                            )
                        }
                        DropdownMenu(showModeMenu, onDismissRequest = { showModeMenu = false }) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.agents_handoff_mode_default)) },
                                onClick = { permissionMode = "default"; showModeMenu = false }
                            )
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.agents_handoff_mode_bypass)) },
                                onClick = { permissionMode = "bypassPermissions"; showModeMenu = false }
                            )
                        }
                    }
                }
            } else {
                JSectionHeader(stringResource(R.string.agents_handoff_pick_agent), count = runningAgents.size)
                if (runningAgents.isEmpty()) {
                    JEmptyState(stringResource(R.string.agents_handoff_no_agents), stringResource(R.string.agents_handoff_no_agents_hint))
                } else {
                    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        runningAgents.forEachIndexed { index, session ->
                            val selected = selectedTarget == session.key
                            val machineName = machines.firstOrNull { it.id == session.machineId }?.displayName ?: session.machineId
                            JGroupItem(GroupPosition.of(index, runningAgents.size)) {
                                JRow(
                                    title = session.displayTitle,
                                    subtitle = stringResource(R.string.agents_todo_location, machineName, session.folderName),
                                    leading = {
                                        val status = Status.entries.firstOrNull { it.name == session.agentState.name } ?: Status.Idle
                                        JStatusDot(status)
                                    },
                                    trailing = if (selected) ({ JIcon(PhIcons.CheckCircle, null, selected = true) }) else null,
                                    chevron = false,
                                    onClick = { selectedTarget = session.key }
                                )
                            }
                        }
                    }
                }
            }

            JSectionHeader(stringResource(R.string.agents_handoff_what))
            JTextField(
                instruction,
                { instruction = it },
                label = stringResource(R.string.agents_handoff_instruction),
                placeholder = stringResource(R.string.agents_handoff_instruction_placeholder),
                minLines = 3,
                maxLines = 6
            )

            JSectionHeader(stringResource(R.string.agents_handoff_sent_along))
            JCard(Modifier.fillMaxWidth()) {
                Row(
                    Modifier.fillMaxWidth().padding(Tokens.Space.s),
                    horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m),
                    verticalAlignment = Alignment.Top
                ) {
                    Box(
                        Modifier
                            .width(4.dp)
                            .height(36.dp)
                            .clip(RoundedCornerShape(2.dp))
                            .background(PersonaTheme.palette.accent)
                    )
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            JIcon(PhIcons.ListChecks, null, size = 16.dp, tint = PersonaTheme.palette.accent)
                            Text(
                                stringResource(R.string.agents_row_todo, todo.title),
                                style = PersonaTheme.type.headline,
                                color = PersonaTheme.palette.ink,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis
                            )
                        }
                        if (todo.notes.isNotBlank()) {
                            Text(
                                todo.notes,
                                style = PersonaTheme.type.caption,
                                color = PersonaTheme.palette.ink2,
                                maxLines = 3,
                                overflow = TextOverflow.Ellipsis
                            )
                        }
                    }
                }
            }

            Spacer(Modifier.height(8.dp))

            error?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.danger.text) }

            JButton(
                text = stringResource(if (starting) R.string.agents_handoff_starting else R.string.agents_todo_hand_to_agent),
                onClick = {
                    val prompt = todoHandOffPrompt(todo.title, todo.notes, instruction)
                    if (mode == HandOffMode.New) {
                        val m = selectedMachine ?: return@JButton
                        onHandOffNew(
                            m.id,
                            cwd.ifEmpty { "~" },
                            launcher,
                            permissionMode.takeIf { it != "default" },
                            prompt
                        )
                    } else {
                        val target = selectedTarget ?: return@JButton
                        onHandOffExisting(target, prompt)
                    }
                },
                enabled = !starting && (if (mode == HandOffMode.New) selectedMachine != null else selectedTarget != null),
                modifier = Modifier.fillMaxWidth()
            )
        }
    }
}
