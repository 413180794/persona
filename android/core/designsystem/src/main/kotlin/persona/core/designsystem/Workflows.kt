package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.WorkflowStatus
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons

// A Claude Code Workflow run (#2935; docs/agent-states.md "Background workflow, Monitor"): the card in an agent's details and
// the icon its one line on the Agents list and the task cards starts with. Words come from the page (core:domain
// WorkflowSummary); this draws them the same in every style.

/** The mark of a background workflow, wherever one is named. */
val WorkflowIcon: PhIcon get() = PhIcons.TreeStructure

/** One of a run's agents: [onClick] opens its transcript as a subagent's; null where it cannot be opened. */
@Immutable
data class WorkflowAgentLine(
    val key: String, val label: String, val phase: String?, val running: Boolean, val lastText: String?,
    val onClick: (() -> Unit)? = null,
)

/**
 * A run: [title] and the [status] chip ([statusText]; running is AI-colored text, done positive, failed danger,
 * stopped neutral and gone quiet caution) with how long it ran ([elapsed]); its [phases] in a row, the [current] one
 * (1-based) lit and the ones before it done; [progress] ("169/185 agents done · 16 running"); [error] on one line;
 * then "Show agents", which lists [agents] while [expanded].
 */
@Composable
fun JWorkflowCard(
    title: String,
    status: WorkflowStatus,
    statusText: String,
    modifier: Modifier = Modifier,
    elapsed: String? = null,
    phases: List<String> = emptyList(),
    current: Int? = null,
    progress: String? = null,
    error: String? = null,
    agents: List<WorkflowAgentLine> = emptyList(),
    expanded: Boolean = false,
    onToggle: () -> Unit = {},
    position: GroupPosition = GroupPosition.Single,
) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    JGroupItem(position, modifier) {
        Column(Modifier.fillMaxWidth().padding(Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                JIcon(WorkflowIcon, null, size = Tokens.Space.l, tint = palette.ai)
                Spacer(Modifier.width(Tokens.Space.s))
                Text(title, Modifier.weight(1f), style = type.label.copy(fontWeight = FontWeight.SemiBold), color = palette.ink,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.width(Tokens.Space.s))
                WorkflowStatusTag(statusText, status)
                elapsed?.let {
                    Spacer(Modifier.width(Tokens.Space.s))
                    Text(it, style = type.micro, color = palette.ink3, maxLines = 1)
                }
            }
            if (phases.isNotEmpty()) PhaseStrip(phases, if (status == WorkflowStatus.Completed) phases.size + 1 else current)
            progress?.let { Text(it, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            error?.takeIf(String::isNotBlank)?.let {
                Text(it, style = type.caption, color = palette.danger.text, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (agents.isNotEmpty()) {
                JButton(stringResource(if (expanded) R.string.designsystem_workflow_hide_agents else R.string.designsystem_workflow_show_agents),
                    onToggle, kind = JButtonKind.Quiet, size = JButtonSize.Small, icon = if (expanded) PhIcons.CaretUp else PhIcons.CaretDown)
                if (expanded) Column {
                    agents.forEachIndexed { index, agent ->
                        if (index > 0) JDivider()
                        WorkflowAgentRow(agent)
                    }
                }
            }
        }
    }
}

/** A readable state label on a neutral surface; only failure is danger. */
@Composable
private fun WorkflowStatusTag(text: String, status: WorkflowStatus) {
    val palette = PersonaTheme.palette
    when (status) {
        WorkflowStatus.Running -> JTagColored(text, Modifier, StatusColors(dot = palette.ai, text = palette.ai, tint = palette.statusSurface))
        WorkflowStatus.Completed -> JTag(text, Tone.Positive)
        WorkflowStatus.Failed -> JTag(text, Tone.Danger)
        WorkflowStatus.Killed -> JTag(text, Tone.Neutral)
        WorkflowStatus.Stalled -> JTag(text, Tone.Caution)
    }
}

/** The phases in order: the ones before [current] done (a check), [current] lit, the rest still to come. */
@Composable
private fun PhaseStrip(phases: List<String>, current: Int?) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Row(Modifier.horizontalScroll(rememberScrollState()), verticalAlignment = Alignment.CenterVertically) {
        phases.forEachIndexed { index, phase ->
            val place = index + 1
            val done = current != null && place < current
            val now = current == place
            if (index > 0) JIcon(PhIcons.CaretRight, null, size = Tokens.Space.m, tint = palette.ink3,
                modifier = Modifier.padding(horizontal = Tokens.Space.xxs))
            Row(
                Modifier.clip(PersonaTheme.shapes.chipShape).background(palette.statusSurface)
                    .padding(horizontal = Tokens.Space.s, vertical = Tokens.Space.xxs),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (done) {
                    JIcon(PhIcons.Check, null, size = Tokens.Space.m, tint = palette.positive)
                    Spacer(Modifier.width(Tokens.Space.xxs))
                }
                Text(phase, style = type.micro.copy(fontWeight = if (now) FontWeight.SemiBold else FontWeight.Medium),
                    color = when { now -> palette.ai; done -> palette.ink2; else -> palette.ink3 }, maxLines = 1)
            }
        }
    }
}

@Composable
private fun WorkflowAgentRow(agent: WorkflowAgentLine) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Row(
        Modifier.fillMaxWidth().then(agent.onClick?.let { Modifier.clickable(role = Role.Button, onClick = it) } ?: Modifier)
            .padding(vertical = Tokens.Space.s),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        JStatusDot(if (agent.running) Status.Working else Status.Idle)
        Spacer(Modifier.width(Tokens.Space.s))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(agent.label, Modifier.weight(1f, fill = false), style = type.caption.copy(fontWeight = FontWeight.Medium), color = palette.ink,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                agent.phase?.takeIf(String::isNotBlank)?.let {
                    Spacer(Modifier.width(Tokens.Space.s))
                    Text(it, style = type.micro, color = palette.ink3, maxLines = 1)
                }
            }
            agent.lastText?.takeIf(String::isNotBlank)?.let {
                Text(it.replace('\n', ' '), style = type.caption.copy(fontWeight = FontWeight.Normal), color = palette.ink2,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        if (agent.onClick != null) JIcon(PhIcons.CaretRight, null, size = Tokens.Space.l, tint = palette.ink3)
    }
}
