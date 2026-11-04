package persona.feature.agents

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTag
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.api.HubNotice
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * The one quiet line over the Agents list about the hub (docs/hub-update.md「10. App 里」, iOS `HubNoticeRow`):
 * app/hub too old, updating, rolled back, auto-update notice, or newer version/image.
 * A tap opens the hub version settings page; buttons inside handle notice-specific actions.
 */
@Composable
internal fun HubNoticeRow(
    notice: HubNotice,
    onClick: () -> Unit,
    onAutoOff: () -> Unit,
    onNoticeSeen: () -> Unit,
    onUpdate: () -> Unit,
    onDismiss: (HubNotice) -> Unit,
    modifier: Modifier = Modifier,
) {
    val palette = PersonaTheme.palette
    val cautionColor = palette.status(Status.NeedsYou).dot
    val accentColor = palette.accent

    val (icon, tint) = when (notice) {
        HubNotice.AppTooOld, HubNotice.HubTooOld, is HubNotice.RolledBack -> PhIcons.Warning to cautionColor
        else -> PhIcons.ArrowsClockwise to accentColor
    }

    JCard(
        modifier = modifier
            .fillMaxWidth()
            .semantics { testTag = "agents.hubNotice" },
        onClick = onClick,
        padding = Tokens.Space.m,
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
        ) {
            if (notice is HubNotice.Updating) {
                JSpinner(
                    modifier = Modifier.size(Tokens.Space.l),
                    color = accentColor,
                )
            } else {
                JIcon(icon, null, tint = tint, decorative = true)
            }

            Text(
                text = HubNotice.text(notice),
                modifier = Modifier.weight(1f),
                style = PersonaTheme.type.callout,
                color = palette.ink,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )

            when (notice) {
                HubNotice.AutoOn -> {
                    JButton("关闭", onAutoOff, kind = JButtonKind.Quiet, size = JButtonSize.Small)
                    JButton("知道了", onNoticeSeen, kind = JButtonKind.Quiet, size = JButtonSize.Small)
                }
                is HubNotice.Available -> {
                    JButton("更新", onUpdate, kind = JButtonKind.Quiet, size = JButtonSize.Small)
                    JIconButton(PhIcons.X, "不再提示", { onDismiss(notice) }, raised = false)
                }
                is HubNotice.RolledBack, HubNotice.HubTooOld, is HubNotice.NewImage -> {
                    JIconButton(PhIcons.X, "不再提示", { onDismiss(notice) }, raised = false)
                }
                HubNotice.AppTooOld, is HubNotice.Updating -> Unit
            }
        }
    }
}
