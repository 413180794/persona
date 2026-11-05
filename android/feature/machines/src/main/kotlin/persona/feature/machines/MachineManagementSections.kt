package persona.feature.machines

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.Machine
import persona.core.contracts.api.AgentLauncher
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.engines.Engines

/** What the machine card says about its personad build, with 重新部署 while it differs from the hub's (iOS `DeployRow`). */
@Composable
internal fun MachineDeployLine(machine: Machine, starting: Boolean, onDeploy: (() -> Unit)?) {
    val busy = machine.deploying || starting
    val text = when {
        busy -> stringResource(R.string.machines_deploying)
        !machine.deployError.isNullOrBlank() -> stringResource(R.string.machines_deploy_failed_last, machine.deployError.orEmpty())
        else -> stringResource(R.string.machines_node_mismatch)
    }
    JInlineStatus(
        text, Modifier.padding(top = Tokens.Space.s),
        tone = when { busy -> InlineTone.Busy; machine.deployError.isNullOrBlank() -> InlineTone.Caution; else -> InlineTone.Failed },
        action = if (busy || onDeploy == null || !machine.online) null else ({
            JButton(stringResource(R.string.machines_redeploy), onDeploy, kind = JButtonKind.Secondary, size = JButtonSize.Small)
        }),
    )
}

/** Whether the card shows the deploy line: a node that is not the hub's build, or one being replaced. */
internal fun showsDeploy(machine: Machine?): Boolean = machine != null && (machine.nodeMismatch == true || machine.deploying)

/**
 * The machine page's controls: its engine (opens the choice), 同时显示 botmux 会话 right under it when the hub has the
 * switch ([botmux] non-null; flipped in place, like the web page's header pill, #2842) and 重新连接, with how the hub
 * reaches it.
 */
internal fun LazyListScope.machineControls(
    machine: Machine,
    reconnecting: Boolean,
    onEngine: () -> Unit,
    onReconnect: () -> Unit,
    botmux: Boolean? = null,
    botmuxSaving: Boolean = false,
    onBotmux: (Boolean) -> Unit = {},
) {
    val rows = if (botmux != null) 3 else 2
    item("controls-title") { JSectionHeader(stringResource(R.string.machines_controls_title)) }
    item("engine") {
        JGroupItem(GroupPosition.of(0, rows)) {
            JRow(stringResource(R.string.machines_engine), leadingIcon = PhIcons.TerminalWindow, value = Engines.currentName(machine), onClick = onEngine)
        }
    }
    if (botmux != null) item("botmux") {
        JGroupItem(GroupPosition.of(1, rows)) {
            JSwitchRow(stringResource(R.string.machines_botmux), botmux, onBotmux, leadingIcon = PhIcons.Stack, enabled = !botmuxSaving)
        }
    }
    item("reconnect") {
        JGroupItem(GroupPosition.of(rows - 1, rows)) {
            JRow(stringResource(if (reconnecting) R.string.machines_reconnecting else R.string.machines_reconnect), leadingIcon = PhIcons.ArrowClockwise, enabled = !reconnecting,
                onClick = onReconnect, chevron = false, trailing = if (reconnecting) ({ JSpinner() }) else null)
        }
    }
    item("controls-note") {
        Column(Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            MachineCaption(stringResource(if (botmux == null) R.string.machines_engine_note else R.string.machines_botmux_note))
            MachineCaption(if (machine.kind == "local") stringResource(R.string.machines_local_note)
                else stringResource(R.string.machines_ssh_note, machine.sshHost?.takeIf { it.isNotBlank() } ?: machine.id))
        }
    }
}

/**
 * 启动器 N: each launcher's name and the model it runs (the last part of a provider path). With [onLauncher] (the hub
 * manages extensions) a row opens the launcher's page, where extensions may draw a section for its gateway.
 */
internal fun LazyListScope.machineLaunchers(online: Boolean, state: MachineActionsState, onLauncher: ((String) -> Unit)? = null) {
    val launchers = state.launchers.orEmpty()
    item("launchers-title") { JSectionHeader(stringResource(R.string.machines_launchers), count = launchers.size.takeIf { online && state.launchers != null }) }
    when {
        !online -> item("launchers-offline") { MachineCaption(stringResource(R.string.machines_launchers_offline), Modifier.padding(horizontal = rowPadding)) }
        state.launchersError != null && launchers.isEmpty() -> item("launchers-error") {
            JInlineStatus(stringResource(R.string.machines_launchers_error, state.launchersError), tone = InlineTone.Failed)
        }
        state.launchersLoading && state.launchers == null -> item("launchers-loading") { JLoading() }
        state.launchers != null && launchers.isEmpty() -> item("launchers-empty") {
            MachineCaption(stringResource(R.string.machines_launchers_empty), Modifier.padding(horizontal = rowPadding))
        }
    }
    itemsIndexed(launchers, key = { _, it -> "launcher-${it.name}" }, contentType = { _, _ -> "launcher" }) { index, launcher ->
        val model = launcherModel(launcher)
        JGroupItem(GroupPosition.of(index, launchers.size)) {
            JRow(chevron = onLauncher != null, onClick = onLauncher?.let { open -> { open(launcher.name) } }, trailing = if (model == null) null else ({
                Text(model, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
            })) {
                Text(launcher.name, style = PersonaTheme.type.code, color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** "claude-opus-4" and "anthropic/claude-opus-4" both read as the last part of the path (iOS `Fmt.model`). */
internal fun launcherModel(launcher: AgentLauncher): String? = launcher.model?.takeIf { it.isNotBlank() }?.substringAfterLast('/')

@Composable
internal fun MachineCaption(text: String, modifier: Modifier = Modifier) {
    Text(text, modifier, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
}
