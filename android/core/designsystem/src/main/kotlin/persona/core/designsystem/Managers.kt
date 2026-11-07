package persona.core.designsystem

import android.content.res.Resources
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.res.stringResource
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerGroup
import persona.core.contracts.api.ManagerRoster
import persona.core.contracts.parseTime
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

// Several managers on one hub (docs/assistant/managers.md step 4): the pages that list what managers own group it by manager,
// and the assistant page switches between them. With one installed manager none of this draws anything.

/**
 * The selected hub's roster of managers, as the app last read it; null until read (or from a hub without managers).
 * Pages read [ManagerRoster.groups] / [ManagerRoster.isMulti] from it: with one manager they draw as before.
 */
val LocalManagerRoster = compositionLocalOf<ManagerRoster?> { null }

/** The group ([persona.core.contracts.GroupKeys] namespace; null: the primary) whose hub [LocalManagerRoster] is. */
val LocalManagerGroup = compositionLocalOf<String?> { null }

/** How long ago, as a manager's 最后同步于 says it: 刚刚, n 分钟前, n 小时前, then the day. */
fun managerAgo(res: Resources, at: Instant, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
    val gap = Duration.between(at, now)
    return when {
        gap.isNegative || gap < Duration.ofMinutes(1) -> res.getString(R.string.designsystem_manager_just_now)
        gap < Duration.ofHours(1) -> gap.toMinutes().toInt().let { res.getQuantityString(R.plurals.designsystem_manager_minutes_ago, it, it) }
        gap < Duration.ofDays(1) -> gap.toHours().toInt().let { res.getQuantityString(R.plurals.designsystem_manager_hours_ago, it, it) }
        else -> LocalizedFormats.format(at, LocalizedFormats.MONTH_DAY, res.configuration.locales[0], zone)
    }
}

/** An offline manager's 「最后同步于 …」, or 「离线」 without a time; null while it is online. */
fun managerOfflineText(res: Resources, manager: HubManager, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String? {
    if (manager.online) return null
    return parseTime(manager.lastSeen)?.let { res.getString(R.string.designsystem_manager_last_synced, managerAgo(res, it, now, zone)) }
        ?: res.getString(R.string.designsystem_manager_offline)
}

/**
 * What a conversation of manager [id] on machine [machineId] says where its composer was while that manager is offline
 * (several managers, the selected group's hub: [group]): 「工作 · 最后同步于 3 分钟前」; null when it can take messages, or
 * with one manager. Wherever the conversation was opened (the inbox, a notification), nothing can be sent to it then.
 */
fun managerOfflineNote(res: Resources, roster: persona.core.contracts.api.ManagerRoster?, group: String?, machineId: String, id: String?): String? {
    if (roster == null || !roster.isMulti || persona.core.contracts.GroupKeys.namespaceOf(machineId) != group) return null
    val manager = roster.manager(roster.ownerOf(id))?.takeIf { !it.isInstalling } ?: return null
    return managerOfflineText(res, manager)?.let { res.getString(R.string.designsystem_manager_offline_note, manager.shownName, it) }
}

/** The name a manager goes by on the pages: its name, else its id. */
val HubManager.shownName: String get() = name.ifBlank { id }

/**
 * A group's heading on a page grouped by manager: one level above the page's section headings (a bigger name, its
 * status dot in every style, the count), greyed with 「最后同步于 …」 at the end while the manager is offline. A manager
 * the roster does not list reads as its id. Its sides line up with [JSectionHeader]'s.
 */
@Composable
fun JManagerHeader(group: ManagerGroup<*>, modifier: Modifier = Modifier, now: Instant = Instant.now(), count: Int? = null) {
    val res = LocalResources.current
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val manager = group.manager
    val online = manager?.online == true
    val offline = manager?.let { managerOfflineText(res, it, now) }
    val side = if (style == AppStyle.Brief) 0.dp else Tokens.Space.xs
    Row(modifier.fillMaxWidth().padding(top = Tokens.Space.xxl, bottom = Tokens.Space.s, start = side, end = side).semantics { heading() },
        verticalAlignment = Alignment.CenterVertically) {
        JStatusDot(if (online) Status.Unread else Status.Offline)
        Spacer(Modifier.width(Tokens.Space.s))
        Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
            Text(manager?.shownName ?: group.id, Modifier.weight(1f, fill = false), style = PersonaTheme.type.headline,
                color = if (online) palette.ink else palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
            count?.let { Spacer(Modifier.width(Tokens.Space.xs)); Text("$it", style = PersonaTheme.type.micro, color = palette.ink3) }
        }
        offline?.let {
            Spacer(Modifier.width(Tokens.Space.s))
            Text(it, style = PersonaTheme.type.caption, color = palette.ink3, maxLines = 1)
        }
    }
}

/**
 * The assistant page's switcher: one chip per installed manager (the default first), the shown one selected; an
 * offline one greyed with its dot. Draws nothing with one manager.
 */
@Composable
fun JManagerSwitcher(roster: ManagerRoster?, shown: String, onChoose: (String) -> Unit, modifier: Modifier = Modifier) {
    if (roster == null || !roster.isMulti) return
    val label = stringResource(R.string.designsystem_manager_switcher)
    Row(modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = pagePadding, vertical = Tokens.Space.xs)
        .semantics { contentDescription = label },
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        roster.installed.forEach { manager ->
            val name = manager.shownName
            val spoken = if (manager.online) name else stringResource(R.string.designsystem_manager_chip_offline, name)
            JChip(name, Modifier.semantics { contentDescription = spoken }, selected = manager.id == shown, onClick = { onChoose(manager.id) },
                status = if (manager.online) Status.Unread else Status.Offline, muted = !manager.online)
        }
    }
}
