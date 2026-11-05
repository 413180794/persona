package persona.core.domain.inbox

import persona.core.domain.zhAny

import org.junit.Assert.assertEquals
import org.junit.Test

/** Where an item without a time goes once each time is parsed before sorting (#932): what the old comparators did. */
class InboxTimeOrderTest {
    private val earlier = "2026-11-02T08:30:00+08:00"
    private val later = "2026-11-02T01:00:00Z" // half an hour after earlier, written with Z

    @Test fun newestFirstPutsUnsetTimesLast() {
        val items = listOf(
            InboxItem("b", updatedAt = null), InboxItem("c", updatedAt = earlier),
            InboxItem("a", updatedAt = null), InboxItem("d", updatedAt = later),
        )
        assertEquals(listOf("d", "c", "a", "b"), (orderHubInbox(items).map { it.id }).zhAny())
    }

    @Test fun doneNewestFirstAndWaitingOldestFirst() {
        val result = buildInboxList(
            items = listOf(
                InboxItem("old", doneAt = earlier), InboxItem("unset", doneAt = "0001-01-01T00:00:00Z"),
                InboxItem("new", doneAt = later),
            ),
            interactions = listOf(
                InboxInteraction("late", "m", "s1", createdAt = later),
                InboxInteraction("none", "m", "s2", createdAt = null),
                InboxInteraction("early", "m", "s3", createdAt = earlier),
            ),
            settings = InboxListSettings(expanded = setOf("done", "action")),
        )
        assertEquals(listOf("new", "old", "unset"), (result.groups.single { it.id == "done" }.rows.map { it.id }).zhAny())
        assertEquals(listOf("none", "early", "late"), (result.groups.single { it.id == "action" }.rows.map { it.id }).zhAny())
    }
}
