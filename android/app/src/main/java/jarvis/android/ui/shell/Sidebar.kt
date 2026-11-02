package jarvis.android.ui.shell

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.data.AgentState
import jarvis.android.data.Session
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.CountBadge
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JarvisMark
import jarvis.android.ui.design.SearchField
import jarvis.android.ui.design.StatusDot

/** What the drawer can open (iOS `ShellPage` and friends). */
data class SidebarActions(
    val openProfile: () -> Unit,
    val openInbox: () -> Unit,
    val openRoutines: () -> Unit,
    val openKnowledge: () -> Unit,
    val openMachines: () -> Unit,
    val openSettings: () -> Unit,
    val newConversation: () -> Unit,
    val openConversation: (String) -> Unit,
)

/**
 * The shell's drawer (iOS `AssistantSidebar` in the shell): the name and search on top; 认识 Jarvis and the places to
 * go (收件箱 with its count, 定时任务, 知识库, 机器); the conversations — 置顶 (the main one first) and 最近 (30, then
 * 查看全部); at the foot the 新对话 pill and 设置.
 */
@Composable
fun Sidebar(
    conversations: List<Session>, model: AssistantModel, current: String?, inboxCount: Int, actions: SidebarActions,
) {
    var searching by rememberSaveable { mutableStateOf(false) }
    var query by rememberSaveable { mutableStateOf("") }
    var showAll by rememberSaveable { mutableStateOf(false) }
    val focus = remember { FocusRequester() }
    Column(Modifier.fillMaxSize().background(JTheme.shell.drawer).statusBarsPadding().navigationBarsPadding()) {
        // Top: the name, search.
        Row(
            Modifier.fillMaxWidth().padding(start = JSpace.xl, end = JSpace.l, top = JSpace.s, bottom = JSpace.s).heightIn(min = JSize.avatarL),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (searching) {
                SearchField(query, { query = it }, "搜索对话", Modifier.weight(1f), focus = focus)
                Text(
                    "取消", style = JTheme.type.body, color = JTheme.colors.accent, maxLines = 1,
                    modifier = Modifier.clip(CircleShape).clickable { query = ""; searching = false }.padding(horizontal = JSpace.m, vertical = JSpace.s),
                )
                androidx.compose.runtime.LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
            } else {
                Text("Jarvis", style = JTheme.type.display, color = JTheme.colors.label, maxLines = 1, modifier = Modifier.weight(1f))
                CircleButton(JIcons.search, "搜索对话", { searching = true }, size = JSize.avatarL)
            }
        }
        LazyColumn(Modifier.weight(1f).padding(horizontal = JSpace.s)) {
            if (query.isBlank()) {
                item(key = "profile") {
                    DrawerRow("认识 Jarvis", onClick = actions.openProfile) { JarvisMark(JSize.icon) }
                }
                item(key = "inbox") { DrawerAction("收件箱", JIcons.inbox, actions.openInbox, count = inboxCount) }
                item(key = "routines") { DrawerAction("定时任务", JIcons.schedule, actions.openRoutines) }
                item(key = "knowledge") { DrawerAction("知识库", JIcons.knowledge, actions.openKnowledge) }
                item(key = "machines") { DrawerAction("机器", JIcons.machines, actions.openMachines) }
                item(key = "divider") {
                    HorizontalDivider(Modifier.padding(horizontal = JSpace.m, vertical = JSpace.s), thickness = JSize.hairline, color = JTheme.colors.separator)
                }
                val pinned = conversations.filter { model.isPinned(it.sessionId) }.sortedByDescending { it.sessionId == model.main }
                val recent = conversations.filterNot { model.isPinned(it.sessionId) }
                if (pinned.isNotEmpty()) {
                    item(key = "h-pinned") {
                        Heading("置顶") { close -> DropdownMenuItem({ Text("全部取消置顶", maxLines = 1) }, { close(); model.unpinAll() }) }
                    }
                    items(pinned, key = { "p/" + it.sessionId }) { s -> ConversationRow(s, model, s.sessionId == current, actions) }
                }
                if (recent.isNotEmpty()) {
                    item(key = "h-recent") {
                        Heading("最近") { close ->
                            DropdownMenuItem({ Text("新对话", maxLines = 1) }, { close(); actions.newConversation() })
                            if (!showAll && recent.size > RECENT) DropdownMenuItem({ Text("查看全部", maxLines = 1) }, { close(); showAll = true })
                        }
                    }
                    val shown = if (showAll) recent else recent.take(RECENT)
                    items(shown, key = { "r/" + it.sessionId }) { s -> ConversationRow(s, model, s.sessionId == current, actions) }
                    if (!showAll && recent.size > RECENT) item(key = "all") {
                        Text(
                            "查看全部 ${recent.size} 个", style = JTheme.type.secondary, color = JTheme.colors.accent, maxLines = 1,
                            modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(JSize.badge)).clickable { showAll = true }
                                .padding(start = JSpace.m + JSize.wellM + JSpace.m, top = JSpace.m, bottom = JSpace.m),
                        )
                    }
                }
            } else {
                val q = query.trim()
                val hits = conversations.filter { s ->
                    model.title(s).contains(q, ignoreCase = true) || s.lastText.orEmpty().contains(q, ignoreCase = true)
                }
                if (hits.isEmpty()) item(key = "none") {
                    Column(Modifier.fillMaxWidth().padding(top = JSpace.xxl), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        Icon(JIcons.search, null, Modifier.size(JSize.icon), tint = JTheme.colors.tertiaryText)
                        Text("没有找到「$q」", style = JTheme.type.secondary, color = JTheme.colors.secondaryText, maxLines = 2)
                    }
                } else {
                    item(key = "count") { Heading("${hits.size} 个对话") }
                    items(hits, key = { "s/" + it.sessionId }) { s -> ConversationRow(s, model, s.sessionId == current, actions) }
                }
            }
        }
        // Foot: 新对话 and 设置.
        Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.xl, vertical = JSpace.m), verticalAlignment = Alignment.CenterVertically) {
            Row(
                Modifier.height(JSize.avatarL).clip(CircleShape).background(JTheme.shell.newChat).clickable(onClick = actions.newConversation)
                    .padding(horizontal = JSpace.xl),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(JSpace.s),
            ) {
                Icon(JIcons.compose, null, Modifier.size(JSize.iconXS), tint = androidx.compose.ui.graphics.Color.White)
                Text("新对话", style = JTheme.type.bodyStrong, color = androidx.compose.ui.graphics.Color.White, maxLines = 1)
            }
            Spacer(Modifier.weight(1f))
            CircleButton(JIcons.settings, "设置", actions.openSettings, size = JSize.avatarL)
        }
    }
}

private const val RECENT = 30

/** A place to go: its symbol in one box (so the words line up), the words, a count. */
@Composable
private fun DrawerAction(title: String, icon: ImageVector, onClick: () -> Unit, count: Int = 0) {
    DrawerRow(title, onClick = onClick, trailing = { if (count > 0) CountBadge(count, color = JTheme.needsYou.dot) }) {
        Icon(icon, null, Modifier.size(JSize.iconS), tint = JTheme.colors.label)
    }
}

@Composable
private fun DrawerRow(title: String, onClick: () -> Unit, trailing: @Composable () -> Unit = {}, lead: @Composable () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box50).clip(RoundedCornerShape(JSize.markS)).clickable(onClick = onClick).padding(horizontal = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.l),
    ) {
        Box(Modifier.width(JSize.icon), contentAlignment = Alignment.Center) { lead() }
        Text(title, style = JTheme.type.body, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        trailing()
    }
}

/** A section's quiet heading with its ⋯ menu. */
@Composable
private fun Heading(text: String, menu: (@Composable (close: () -> Unit) -> Unit)? = null) {
    var open by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().padding(start = JSpace.m, top = JSpace.l, bottom = JSpace.xs), verticalAlignment = Alignment.CenterVertically) {
        Text(text, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1, modifier = Modifier.weight(1f))
        if (menu != null) Box {
            Box(Modifier.size(JSize.tapTarget, JSize.wellXS).clip(CircleShape).clickable { open = true }, contentAlignment = Alignment.Center) {
                Icon(JIcons.more, "更多", Modifier.size(JSize.markL), tint = JTheme.colors.tertiaryText)
            }
            DropdownMenu(open, onDismissRequest = { open = false }) { menu { open = false } }
        }
    }
}

/**
 * One conversation: a round mark (the main one is Jarvis' own; the others a soft colored bubble picked by their id,
 * neighbors differing), its title, and a dot only when it needs you, finished unseen or works. The one on screen sits
 * on a gray pill. A long press: 置顶 / 取消置顶, 拷贝会话 ID.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun ConversationRow(s: Session, model: AssistantModel, current: Boolean, actions: SidebarActions) {
    var menu by remember { mutableStateOf(false) }
    val ctx = LocalContext.current
    Box {
        Row(
            Modifier.fillMaxWidth().heightIn(min = JSize.avatarXL).clip(RoundedCornerShape(JSize.markS))
                .then(if (current) Modifier.background(JTheme.colors.fillChip) else Modifier)
                .combinedClickable(onClick = { actions.openConversation(s.sessionId) }, onLongClick = { menu = true })
                .padding(horizontal = JSpace.m),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            if (s.sessionId == model.main) JarvisMark(JSize.wellM) else ConversationMark(s.sessionId)
            Text(model.title(s), style = JTheme.type.body, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            when (s.agentState) {
                AgentState.NeedsYou, AgentState.Working -> StatusDot(s.agentState, JSize.dot)
                AgentState.Unread -> Box(Modifier.size(JSize.dot).clip(CircleShape).background(JTheme.colors.accent))
                else -> Unit
            }
        }
        DropdownMenu(menu, onDismissRequest = { menu = false }) {
            if (s.sessionId != model.main) DropdownMenuItem(
                { Text(if (model.isPinned(s.sessionId)) "取消置顶" else "置顶", maxLines = 1) },
                { menu = false; model.togglePin(s.sessionId) },
                leadingIcon = { Icon(JIcons.pin, null) },
            )
            DropdownMenuItem({ Text("拷贝会话 ID", maxLines = 1) }, {
                menu = false
                jarvis.android.ui.media.MediaShare.copyText(ctx, s.sessionId, "会话 ID")
            }, leadingIcon = { Icon(JIcons.copy, null) })
        }
    }
}

/** A conversation's round mark: one of four soft hues by its id. */
@Composable
private fun ConversationMark(id: String) {
    val hues = JTheme.hue.all
    val hue = hues[(id.hashCode() and 0x7fffffff) % hues.size]
    Box(
        Modifier.size(JSize.wellM).clip(CircleShape).background(hue.copy(alpha = 0.12f)).border(JSize.hairline, hue.copy(alpha = 0.25f), CircleShape),
        contentAlignment = Alignment.Center,
    ) { Icon(JIcons.conversation, null, Modifier.size(JSize.markS), tint = hue) }
}
