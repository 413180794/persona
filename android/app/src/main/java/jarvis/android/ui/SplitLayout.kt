package jarvis.android.ui

import jarvis.android.data.SessionKey

/**
 * Foldables and wide screens (docs/android.md「折叠屏 / 宽屏」, iOS `SplitNavigation`): plain logic, no Compose.
 *
 * The layout goes by the window's width only — never the device or its physical screen. Compact (a phone, the Find N5
 * folded) is the single stack it always was; medium and expanded put the list on the left and what it opened on the
 * right, like a mail app. Both layouts read the same [NavState], so folding or unfolding keeps the page that was open.
 */
enum class WidthClass {
    Compact, Medium, Expanded;

    companion object {
        /** Material's window size classes: under 600 dp compact, under 840 dp medium, else expanded. */
        fun of(widthDp: Float): WidthClass = when {
            widthDp < 600f -> Compact
            widthDp < 840f -> Medium
            else -> Expanded
        }
    }
}

/** A fold that separates the window into two areas side by side, in window dp (left and right edge of the hinge). */
data class Hinge(val startDp: Float, val endDp: Float)

/** Two panes: the list from 0 to [listDp], the detail from [detailStartDp] to the window's right edge. */
data class Panes(val listDp: Float, val detailStartDp: Float) {
    val gapDp get() = detailStartDp - listDp
}

object SplitLayout {
    /** Chat and the Jarvis home stop growing here and centre (iOS `ChatView.readableWidth`). */
    const val READABLE_DP = 760f
    const val LIST_MIN_DP = 300f
    const val LIST_MAX_DP = 400f
    /** The list's share of the window when no fold decides. */
    const val LIST_SHARE = 0.4f
    /** Width of the line between the panes. */
    const val DIVIDER_DP = 1f
    /** A pane narrower than this beside a fold is too small to use: then the fold doesn't decide the split. */
    const val HINGE_PANE_MIN_DP = 240f

    /** Null: one pane (compact width). A separating vertical fold splits the panes on it; otherwise the list takes 40 % (300…400 dp). */
    fun panes(widthDp: Float, hinge: Hinge?): Panes? {
        if (WidthClass.of(widthDp) == WidthClass.Compact) return null
        if (hinge != null && hinge.startDp >= HINGE_PANE_MIN_DP && widthDp - hinge.endDp >= HINGE_PANE_MIN_DP && hinge.endDp >= hinge.startDp) {
            return Panes(hinge.startDp, hinge.endDp)
        }
        val list = (widthDp * LIST_SHARE).coerceIn(LIST_MIN_DP, LIST_MAX_DP)
        return Panes(list, list + DIVIDER_DP)
    }
}

/** What a pane shows. */
sealed interface Page {
    data object AgentsList : Page
    data class Space(val id: String) : Page
    data class Chat(val key: SessionKey) : Page
    data object SettingsMenu : Page
    data class Settings(val path: String) : Page
    data object Jarvis : Page
    data object InboxList : Page
    data class InboxItem(val id: String) : Page
}

/** The placeholder of an empty detail pane. */
enum class EmptyDetail(val title: String, val message: String) {
    Agents("选一个 Agent", "左边点一个 Agent，对话在这里打开"),
    Inbox("选一件事", "左边点一件事，详情和回复在这里"),
    Settings("选一项设置", "左边点一项，在这里打开"),
}

/**
 * The navigation state both layouts share (Root keeps each field in saved state). Single pane it is a stack —
 * the list, then the space page, then the chat; 设置 and its sub-pages; the inbox with its detail over it. Two panes
 * draw the stack's bottom on the left and its top on the right.
 */
data class NavState(
    val tab: Tab = Tab.Agents,
    val settings: Boolean = false,
    val settingsStack: List<String> = emptyList(),
    val space: String? = null,
    val chat: SessionKey? = null,
    val inbox: String? = null,
) {
    /**
     * The open chat belongs to Agents (or to 设置, when a machine's page opened it): two panes the bar stays visible,
     * so another tab can be chosen with a chat open; it shows again on going back to Agents.
     */
    val chatShown: SessionKey? get() = chat?.takeIf { settings || tab == Tab.Agents }

    /** Single pane: the page on screen (the inbox detail is a sheet over its list). */
    val singleTop: Page get() = chatShown?.let(Page::Chat) ?: when {
        settings -> settingsStack.lastOrNull()?.let(Page::Settings) ?: Page.SettingsMenu
        tab == Tab.Jarvis -> Page.Jarvis
        tab == Tab.Inbox -> inbox?.let(Page::InboxItem) ?: Page.InboxList
        space != null -> Page.Space(space)
        else -> Page.AgentsList
    }

    /** Two panes: the list on the left. Jarvis keeps its own conversation list beside the conversation. */
    val list: Page get() = when {
        settings -> Page.SettingsMenu
        tab == Tab.Jarvis -> Page.Jarvis
        tab == Tab.Inbox -> Page.InboxList
        else -> Page.AgentsList
    }

    /** Two panes: the right pane's page (null: the placeholder; Jarvis draws its own). */
    val detail: Page? get() = chatShown?.let(Page::Chat) ?: when {
        settings -> settingsStack.lastOrNull()?.let(Page::Settings)
        tab == Tab.Jarvis -> null
        tab == Tab.Inbox -> inbox?.let(Page::InboxItem)
        else -> space?.let(Page::Space)
    }

    /** The placeholder for an empty right pane. */
    val emptyDetail: EmptyDetail get() = when {
        settings -> EmptyDetail.Settings
        tab == Tab.Inbox -> EmptyDetail.Inbox
        else -> EmptyDetail.Agents
    }

    /** The right pane's page is the first thing the list opened: nothing to go back to (the list is beside it), so no back button. */
    val detailIsFirst: Boolean get() = when {
        chatShown != null -> if (settings) settingsStack.isEmpty() else space == null
        settings -> settingsStack.size == 1
        else -> true
    }

    // Opening from the list replaces what the right pane shows; single pane the list is the stack's bottom with
    // nothing over it, so replacing is the same as pushing.

    fun openChatFromList(key: SessionKey) = copy(space = null, chat = key)
    fun openSpaceFromList(id: String) = copy(space = id, chat = null)
    fun openSettingsFromMenu(path: String) = copy(settingsStack = listOf(path), chat = null)
    fun openInboxFromList(id: String) = copy(inbox = id)

    // Opened from inside the detail: pushed, back returns to it.

    fun openChatFromDetail(key: SessionKey) = copy(chat = key)
    fun pushSettings(path: String) = copy(settingsStack = settingsStack + path)

    /** Back (the system gesture): pops the top page; null when there is nothing to pop (the app goes to the background). */
    fun back(): NavState? = when {
        chatShown != null -> copy(chat = null)
        settings -> if (settingsStack.isNotEmpty()) copy(settingsStack = settingsStack.dropLast(1)) else copy(settings = false)
        tab == Tab.Inbox && inbox != null -> copy(inbox = null)
        tab == Tab.Agents && space != null -> copy(space = null)
        else -> null
    }

    /** A tab in the bar: two panes the bar is beside 设置 too, so choosing a tab leaves it (an open chat stays with Agents). */
    fun selectTab(t: Tab) = copy(tab = t, settings = false)

    /** A chat opened from anywhere (a notification, the inbox, Jarvis): it shows in Agents, or over 设置 when that is open. */
    fun openChat(key: SessionKey) = copy(chat = key, tab = Tab.Agents)
}
