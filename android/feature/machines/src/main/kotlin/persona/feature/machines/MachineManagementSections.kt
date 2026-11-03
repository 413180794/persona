package persona.feature.machines

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.Machine
import persona.core.contracts.api.AgentLauncher
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.engines.Engines

/** What the machine card says about its jarvisd build, with 重新部署 while it differs from the hub's (iOS `DeployRow`). */
@Composable
internal fun MachineDeployLine(machine: Machine, starting: Boolean, onDeploy: (() -> Unit)?) {
    val busy = machine.deploying || starting
    val text = when {
        busy -> "正在把 hub 这一版的 jarvisd 装到这台电脑…"
        !machine.deployError.isNullOrBlank() -> "上次部署没成：${machine.deployError}"
        else -> "jarvisd 和 hub 不是同一次部署"
    }
    JInlineStatus(
        text, Modifier.padding(top = Tokens.Space.s),
        tone = when { busy -> InlineTone.Busy; machine.deployError.isNullOrBlank() -> InlineTone.Caution; else -> InlineTone.Failed },
        action = if (busy || onDeploy == null || !machine.online) null else ({
            JButton("重新部署", onDeploy, kind = JButtonKind.Secondary, size = JButtonSize.Small)
        }),
    )
}

/** Whether the card shows the deploy line: a node that is not the hub's build, or one being replaced. */
internal fun showsDeploy(machine: Machine?): Boolean = machine != null && (machine.nodeMismatch == true || machine.deploying)

/** The machine page's controls: its engine (opens the choice) and 重新连接, with how the hub reaches it. */
internal fun LazyListScope.machineControls(
    machine: Machine,
    reconnecting: Boolean,
    onEngine: () -> Unit,
    onReconnect: () -> Unit,
) {
    item("controls-title") { JSectionHeader("引擎与连接") }
    item("engine") {
        JGroupItem(GroupPosition.of(0, 2)) {
            JRow("引擎", leadingIcon = PhIcons.TerminalWindow, value = Engines.currentName(machine), onClick = onEngine)
        }
    }
    item("reconnect") {
        JGroupItem(GroupPosition.of(1, 2)) {
            JRow(if (reconnecting) "正在重新连接…" else "重新连接", leadingIcon = PhIcons.ArrowClockwise, enabled = !reconnecting,
                onClick = onReconnect, chevron = false, trailing = if (reconnecting) ({ JSpinner() }) else null)
        }
    }
    item("controls-note") {
        Column(Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            MachineCaption("只显示所选引擎里的会话。")
            MachineCaption(if (machine.kind == "local") "hub 所在的机器，直接运行 node。"
                else "hub 通过 ssh ${machine.sshHost?.takeIf { it.isNotBlank() } ?: machine.id} 接入。")
        }
    }
}

/** 启动器 N: each launcher's name and the model it runs (the last part of a provider path). */
internal fun LazyListScope.machineLaunchers(online: Boolean, state: MachineActionsState) {
    val launchers = state.launchers.orEmpty()
    item("launchers-title") { JSectionHeader("启动器", count = launchers.size.takeIf { online && state.launchers != null }) }
    when {
        !online -> item("launchers-offline") { MachineCaption("机器离线，连上后显示", Modifier.padding(horizontal = rowPadding)) }
        state.launchersError != null && launchers.isEmpty() -> item("launchers-error") {
            JInlineStatus(state.launchersError, tone = InlineTone.Failed)
        }
        state.launchersLoading && state.launchers == null -> item("launchers-loading") { JLoading() }
        state.launchers != null && launchers.isEmpty() -> item("launchers-empty") {
            MachineCaption("这台机器没有可用的启动器", Modifier.padding(horizontal = rowPadding))
        }
    }
    itemsIndexed(launchers, key = { _, it -> "launcher-${it.name}" }, contentType = { _, _ -> "launcher" }) { index, launcher ->
        val model = launcherModel(launcher)
        JGroupItem(GroupPosition.of(index, launchers.size)) {
            JRow(chevron = false, trailing = if (model == null) null else ({
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
