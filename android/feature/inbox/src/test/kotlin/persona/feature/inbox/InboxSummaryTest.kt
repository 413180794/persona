package persona.feature.inbox

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.domain.inbox.InboxItem
import persona.core.domain.inbox.InboxListSettings
import persona.core.domain.inbox.InboxRow
import persona.core.domain.inbox.buildInboxList
import org.junit.Assert.assertEquals
import org.junit.Test

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class InboxSummaryTest {
    /** The app's strings; Robolectric runs in Simplified Chinese (robolectric.properties). */
    private val res get() = androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources
    private val items = listOf(
        InboxItem("reply", kind = "dm", title = "要回的", tier = "action", updatedAt = "2026-11-02T10:00:00Z"),
        InboxItem("busy", kind = "dm", title = "Persona 在办的", tier = "action", updatedAt = "2026-11-02T09:00:00Z"),
        InboxItem("busy-fyi", kind = "mail", title = "在办的通知", tier = "fyi", updatedAt = "2026-11-02T08:00:00Z"),
        InboxItem("read", kind = "mail", title = "看过的", tier = "action", updatedAt = "2026-11-02T07:00:00Z",
            doneAt = "2026-11-02T07:30:00Z"),
    )
    private fun working(row: InboxRow) = row.id.startsWith("busy")

    @Test fun rowsAnAgentWorksOnArePersonasNotTheUsers() {
        assertEquals("1 条要回，Persona 在办 2 件", inboxSummary(res, buildInboxList(items), ::working))
    }

    @Test fun nothingLeftToAnswerIsAllDone() {
        assertEquals("都处理完了", inboxSummary(res, buildInboxList(items.filter { it.id != "reply" }), ::working))
    }

    @Test fun aSearchThatShowsNothingWouldReadAsAllDone() {
        // Why the page hands it the unfiltered list: the searched list has no rows left to count.
        val searched = buildInboxList(items, settings = InboxListSettings(query = "没有这个"))
        assertEquals("都处理完了", inboxSummary(res, searched, ::working))
        assertEquals("1 条要回，Persona 在办 2 件", inboxSummary(res, buildInboxList(items), ::working))
    }
}
