package jarvis.android

import jarvis.android.data.SessionKey
import jarvis.android.ui.EmptyDetail
import jarvis.android.ui.Hinge
import jarvis.android.ui.NavState
import jarvis.android.ui.Page
import jarvis.android.ui.Panes
import jarvis.android.ui.SplitLayout
import jarvis.android.ui.Tab
import jarvis.android.ui.WidthClass
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Foldables and wide screens (docs/android.md「折叠屏 / 宽屏」): the layout by width, where pages go, fold / unfold. */
class SplitTest {
    private val a = SessionKey("mac", "a")
    private val b = SessionKey("mac", "b")

    @Test fun widthClasses() {
        assertEquals(WidthClass.Compact, WidthClass.of(411f)) // a phone, the Find N5 folded
        assertEquals(WidthClass.Compact, WidthClass.of(599.9f))
        assertEquals(WidthClass.Medium, WidthClass.of(600f))
        assertEquals(WidthClass.Medium, WidthClass.of(749f)) // a book-style foldable open
        assertEquals(WidthClass.Expanded, WidthClass.of(840f))
    }

    @Test fun compactIsOnePane() {
        assertNull(SplitLayout.panes(411f, null))
        // Even with a fold reported: the width decides.
        assertNull(SplitLayout.panes(560f, Hinge(280f, 280f)))
    }

    @Test fun listWidthWithoutAFold() {
        assertEquals(Panes(300f, 301f), SplitLayout.panes(600f, null)) // 40 % is 240: the minimum
        assertEquals(Panes(360f, 361f), SplitLayout.panes(900f, null))
        assertEquals(Panes(400f, 401f), SplitLayout.panes(1280f, null)) // the maximum
    }

    @Test fun aSeparatingFoldSplitsThePanes() {
        assertEquals(Panes(374f, 374f), SplitLayout.panes(749f, Hinge(374f, 374f)))
        // A fold with width (two screens): the detail starts after it, nothing sits on the hinge.
        val p = SplitLayout.panes(900f, Hinge(440f, 460f))!!
        assertEquals(440f, p.listDp)
        assertEquals(20f, p.gapDp)
        // A fold too close to an edge leaves a pane too small to use: split as without one.
        assertEquals(Panes(300f, 301f), SplitLayout.panes(700f, Hinge(100f, 100f)))
    }

    @Test fun onePaneStack() {
        assertEquals(Page.AgentsList, NavState().singleTop)
        assertEquals(Page.Space("w"), NavState(space = "w").singleTop)
        assertEquals(Page.Chat(a), NavState(space = "w", chat = a).singleTop)
        assertEquals(Page.SettingsMenu, NavState(settings = true).singleTop)
        assertEquals(Page.Settings("machine/m"), NavState(settings = true, settingsStack = listOf("machines", "machine/m")).singleTop)
        assertEquals(Page.Chat(a), NavState(settings = true, settingsStack = listOf("machine/m"), chat = a).singleTop)
        assertEquals(Page.InboxItem("i"), NavState(tab = Tab.Inbox, inbox = "i").singleTop)
        assertEquals(Page.Jarvis, NavState(tab = Tab.Jarvis).singleTop)
    }

    @Test fun twoPanesListAndDetail() {
        NavState().let { assertEquals(Page.AgentsList, it.list); assertNull(it.detail); assertEquals(EmptyDetail.Agents, it.emptyDetail) }
        NavState(space = "w", chat = a).let { assertEquals(Page.AgentsList, it.list); assertEquals(Page.Chat(a), it.detail) }
        NavState(settings = true).let { assertEquals(Page.SettingsMenu, it.list); assertNull(it.detail); assertEquals(EmptyDetail.Settings, it.emptyDetail) }
        NavState(tab = Tab.Inbox).let { assertEquals(Page.InboxList, it.list); assertEquals(EmptyDetail.Inbox, it.emptyDetail) }
        NavState(tab = Tab.Inbox, inbox = "i").let { assertEquals(Page.InboxItem("i"), it.detail) }
        assertEquals(Page.Jarvis, NavState(tab = Tab.Jarvis).list)
    }

    @Test fun fromTheListReplacesFromTheDetailPushes() {
        // A space open on the right, then a chat from the list: the chat replaces it (back leaves the pane empty).
        val fromList = NavState(space = "w").openChatFromList(a)
        assertEquals(NavState(chat = a), fromList)
        assertTrue(fromList.detailIsFirst)
        assertEquals(NavState(), fromList.back())
        // The same chat from the space page: pushed, back returns to the space.
        val fromSpace = NavState(space = "w").openChatFromDetail(a)
        assertFalse(fromSpace.detailIsFirst)
        assertEquals(NavState(space = "w"), fromSpace.back())
        // A space from the list closes the chat that was open.
        assertEquals(NavState(space = "v"), NavState(space = "w", chat = a).openSpaceFromList("v"))
        assertTrue(NavState(space = "v").detailIsFirst)
        // Another chat from the list replaces the first.
        assertEquals(NavState(chat = b), NavState(chat = a).openChatFromList(b))
    }

    @Test fun settingsMenuReplacesPagesPush() {
        val s = NavState(settings = true, settingsStack = listOf("machines", "machine/m"))
        val menu = s.openSettingsFromMenu("devices")
        assertEquals(listOf("devices"), menu.settingsStack)
        assertTrue(menu.detailIsFirst)
        val pushed = menu.pushSettings("x")
        assertFalse(pushed.detailIsFirst)
        // A chat from a machine's page goes over it; back returns to the page, then pops, then leaves 设置.
        val chat = NavState(settings = true, settingsStack = listOf("machines", "machine/m")).openChatFromDetail(a)
        assertEquals(Page.Chat(a), chat.detail)
        assertFalse(chat.detailIsFirst)
        assertEquals(listOf("machines", "machine/m"), chat.back()!!.settingsStack)
        assertEquals(listOf("machines"), chat.back()!!.back()!!.settingsStack)
        assertEquals(NavState(), NavState(settings = true).back())
    }

    @Test fun onePaneSettingsOpensAreTheSameAsPushes() {
        // Single pane the menu shows only with nothing over it, where replacing and pushing agree.
        val root = NavState(settings = true)
        assertEquals(root.pushSettings("machines"), root.openSettingsFromMenu("machines"))
        assertEquals(NavState().openChatFromDetail(a), NavState().openChatFromList(a))
        assertEquals(NavState(tab = Tab.Agents, space = "w"), NavState().openSpaceFromList("w"))
    }

    @Test fun foldingKeepsThePage() {
        // Whatever the right pane shows is the single stack's top: unfolding then folding keeps the page open.
        val states = listOf(
            NavState(chat = a), NavState(space = "w"), NavState(space = "w", chat = a),
            NavState(settings = true, settingsStack = listOf("machines")), NavState(settings = true, settingsStack = listOf("machine/m"), chat = a),
            NavState(tab = Tab.Inbox, inbox = "i"),
        )
        for (s in states) assertEquals(s.toString(), s.singleTop, s.detail)
        // Nothing open on the right: single pane shows the list itself.
        for (s in listOf(NavState(), NavState(settings = true), NavState(tab = Tab.Inbox))) assertEquals(s.list, s.singleTop)
    }

    @Test fun anotherTabKeepsTheChatForAgents() {
        // Two panes the bar stays beside a chat: choosing 收件箱 hides it, coming back to Agents shows it again.
        val inbox = NavState(chat = a).selectTab(Tab.Inbox)
        assertEquals(a, inbox.chat)
        assertNull(inbox.chatShown)
        assertEquals(Page.InboxList, inbox.list)
        assertNull(inbox.detail)
        assertNull(inbox.back())
        assertEquals(Page.Chat(a), inbox.selectTab(Tab.Agents).detail)
        // Folded on 收件箱 meanwhile: the single stack shows the inbox, not the hidden chat.
        assertEquals(Page.InboxList, inbox.singleTop)
        // A tab leaves 设置.
        assertFalse(NavState(settings = true).selectTab(Tab.Jarvis).settings)
        // A chat opened from anywhere shows in Agents.
        assertEquals(Page.Chat(b), NavState(tab = Tab.Jarvis).openChat(b).detail)
    }

    @Test fun backOrder() {
        assertEquals(NavState(tab = Tab.Inbox), NavState(tab = Tab.Inbox, inbox = "i").back())
        assertEquals(NavState(), NavState(space = "w").back())
        assertNull(NavState().back())
        assertNull(NavState(tab = Tab.Jarvis).back())
    }
}
