package persona.feature.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.api.LauncherQuota
import persona.core.contracts.api.QuotaPool
import persona.core.contracts.api.QuotaUsage
import persona.core.contracts.api.QuotaWindow
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * "Quota" on a launcher's page (docs/integrations/launchers.md "Quota", iOS LauncherQuotaSection, web LauncherQuota): what its key may
 * still spend at its gateway, titled with the gateway's name the hub sends — a budget on the key, each account / provider
 * with its windows (models folded under it), the key's own use; ↻ reads again. Nothing in it names a gateway.
 */
internal fun LazyListScope.launcherQuotaItems(state: LauncherQuotaState, machineName: (String) -> String, onRefresh: () -> Unit, key: String = "quota") {
    val answer = state.answer
    val q = answer?.quota
    item("$key-title") {
        JSectionHeader(QuotaText.title(LocalResources.current, q)) {
            if (answer?.unsupported != true) {
                if (state.loading && answer != null) JSpinner()
                else JIconButton(PhIcons.ArrowClockwise, stringResource(R.string.settings_src_quota_refresh), onRefresh, enabled = !state.loading)
            }
        }
    }
    val rows: List<@Composable () -> Unit> = when {
        state.failed != null && answer == null -> listOf { QuotaLine(state.failed.text(), tone = Tone.Danger) }
        answer == null -> listOf { QuotaLoading() }
        q == null -> listOf { QuotaLine(answer.error?.takeIf { it.isNotBlank() } ?: stringResource(R.string.settings_src_quota_unreadable)) }
        else -> reportRows(q)
    }
    itemsIndexed(rows, key = { index, _ -> "$key-row-$index" }) { index, row ->
        JGroupItem(GroupPosition.of(index, rows.size)) { row() }
    }
    // A failed ↻ keeps the last answer above and says why under it.
    if (state.failed != null && answer != null) item("$key-failed") {
        JInlineStatus(state.failed.text(), Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed)
    }
    if (answer != null && q != null && QuotaText.hasFooter(answer)) item("$key-footer") {
        SettingsCaption(QuotaText.footer(LocalResources.current, answer, machineName), Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    }
}

private fun reportRows(q: LauncherQuota): List<@Composable () -> Unit> = buildList {
    q.note?.takeIf { it.isNotBlank() }?.let { note -> add { QuotaLine(note) } }
    q.budget.forEach { w -> add { QuotaBlock { QuotaWindowRow(w) } } }
    q.pools.forEach { p -> add { QuotaPoolRow(p) } }
    if (q.usage.isNotEmpty()) add { QuotaUsageRows(q.usage) }
    if (QuotaText.unlimited(q)) add { QuotaLine(stringResource(R.string.settings_src_quota_unlimited)) }
}

@Composable
private fun QuotaBlock(content: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        content()
    }
}

@Composable
private fun QuotaLine(text: String, tone: Tone = Tone.Neutral) {
    val palette = PersonaTheme.palette
    Text(text, Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), style = PersonaTheme.type.callout,
        color = if (tone == Tone.Neutral) palette.ink2 else palette.tone(tone).text)
}

@Composable
private fun QuotaLoading() {
    Row(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalAlignment = Alignment.CenterVertically) {
        JSpinner()
        Spacer(Modifier.width(Tokens.Space.s))
        Text(stringResource(R.string.settings_src_quota_loading), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
    }
}

/** An account or provider: its name and state, then its windows; per-model windows folded under it. */
@Composable
private fun QuotaPoolRow(pool: QuotaPool) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    var open by rememberSaveable(pool.name) { mutableStateOf(false) }
    val expand = stringResource(if (open) R.string.settings_src_quota_collapse else R.string.settings_src_quota_expand)
    val expanded = stringResource(if (open) R.string.settings_src_quota_expanded else R.string.settings_src_quota_collapsed)
    QuotaBlock {
        BoxWithConstraints(Modifier.fillMaxWidth()) {
            val detailWidth = maxWidth * 0.6f
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
                // The name takes what the state in words leaves; the state sits at the end, at most 60 % of the row.
                Text(pool.name, Modifier.weight(1f), style = type.body, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                pool.detail?.takeIf { it.isNotBlank() }?.let {
                    Text(it, Modifier.widthIn(max = detailWidth).padding(start = Tokens.Space.s), style = type.caption, maxLines = 2,
                        textAlign = TextAlign.End, color = if (QuotaText.trouble(pool)) palette.tone(Tone.Caution).text else palette.ink3)
                }
            }
        }
        pool.windows.forEach { QuotaWindowRow(it) }
        if (pool.models.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().heightIn(min = Tokens.MinTouch.android)
                .clickable(role = Role.Button, onClickLabel = expand) { open = !open }
                .semantics { stateDescription = expanded }, verticalAlignment = Alignment.CenterVertically) {
                JIcon(if (open) PhIcons.CaretDown else PhIcons.CaretRight, null, tint = palette.ink2, size = Tokens.Space.m)
                Spacer(Modifier.width(Tokens.Space.xs))
                Text(stringResource(R.string.settings_src_quota_by_model, pool.models.size), style = type.caption, color = palette.ink2, maxLines = 1)
            }
            if (open) Column(Modifier.padding(start = Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                pool.models.forEach { m ->
                    Text(m.model, style = type.codeSmall, color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    m.windows.forEach { QuotaWindowRow(it) }
                }
            }
        }
    }
}

/** One window: its name, how much is used (of what), a bar, when it resets. */
@Composable
private fun QuotaWindowRow(w: QuotaWindow) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val res = LocalResources.current
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(w.label, Modifier.weight(1f), style = type.callout, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.width(Tokens.Space.s))
            Text(QuotaText.amount(res, w), style = type.callout, maxLines = 1,
                color = if (w.exhausted) palette.tone(Tone.Danger).text else palette.ink3)
        }
        w.usedRatio?.let { r ->
            val percent = QuotaText.percent(r)
            val description = stringResource(R.string.settings_src_quota_bar_description, w.label, percent)
            JLinearProgress(percent / 100f, Modifier.semantics { contentDescription = description },
                tone = QuotaText.tone(r, w.exhausted))
        }
        w.resetsAt?.let { QuotaText.reset(res, it) }?.takeIf { it.isNotEmpty() }?.let {
            Text(it, style = type.micro, color = palette.ink3, maxLines = 1)
        }
    }
}

@Composable
private fun QuotaUsageRows(usage: List<QuotaUsage>) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    QuotaBlock {
        Text(stringResource(R.string.settings_src_quota_key_usage), style = type.caption, color = palette.ink2, maxLines = 1)
        val res = LocalResources.current
        usage.forEach { u ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(u.label, style = type.callout, color = palette.ink2, maxLines = 1)
                Text(QuotaText.usage(res, u), Modifier.weight(1f).padding(start = Tokens.Space.m), style = type.callout, color = palette.ink3,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.End)
            }
        }
    }
}
