package persona.core.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.InboxAssistantTransport
import java.lang.reflect.Proxy
import java.io.File

class InboxGroupNamesTest {
    private fun json(value: String) = Json.parseToJsonElement(value).jsonObject
    private fun item(kind: String = "group", account: Int? = null) = json("""
        {"kind":"$kind","title":"Event title","sender":"Sender","summary":"Summary",
         "lark":{"feed_id":"feed","is_group":${kind != "dm"}${account?.let { ",\"ol_account\":$it" } ?: ""}}}
    """)
    private fun transport(scope: String = "hub", page: (Int?, String?) -> JsonObject): InboxAssistantTransport =
        Proxy.newProxyInstance(InboxAssistantTransport::class.java.classLoader, arrayOf(InboxAssistantTransport::class.java)) { _, method, args ->
            when (method.name) {
                "getScope" -> scope
                "larkConversationsForAccount" -> page(args[0] as Int?, args[1] as String?)
                else -> error("Unexpected call: ${method.name}")
            }
        } as InboxAssistantTransport

    @Test fun paginatedNamesAreAccountScopedAndNeverReplaceEventFields() = runBlocking {
        val resolver = InboxGroupNames()
        val requests = mutableListOf<Pair<Int?, String?>>()
        val rows = listOf(item(), item(account = 2), item("dm"), item("mail"))
        val original = rows.toString()
        val transport = transport { account, cursor ->
            requests += account to cursor
            if (cursor == null) json("""{"conversations":[],"next_cursor":"next"}""")
            else json("""{"conversations":[{"id":"feed","name":"Group ${account ?: 1}","is_group":true}]}""")
        }
        resolver.refresh(rows, transport)
        assertEquals("Group 1", resolver.name(rows[0], "hub"))
        assertEquals("Group 2", resolver.name(rows[1], "hub"))
        assertNull(resolver.name(rows[2], "hub"))
        assertNull(resolver.name(rows[3], "hub"))
        assertNull(resolver.name(rows[0], "other hub"))
        assertEquals(original, rows.toString())
        resolver.refresh(rows, transport)
        assertEquals(4, requests.size)
    }

    @Test fun absentBlankAndFailedMetadataNeverBecomeTitles() = runBlocking {
        for (response in listOf("{}", """{"conversations":[{"id":"feed","name":"  ","is_group":true}]}""",
            """{"conversations":[{"id":"feed","name":"Person","is_group":false}]}""")) {
            val resolver = InboxGroupNames()
            resolver.refresh(listOf(item()), transport { _, _ -> json(response) })
            assertNull(resolver.name(item(), "hub"))
        }
        val resolver = InboxGroupNames()
        resolver.refresh(listOf(item()), transport { _, _ -> throw IllegalStateException("offline") })
        assertNull(resolver.name(item(), "hub"))
    }

    @Test fun refreshPicksUpRenamesAndDoesNotSwallowCancellation() = runBlocking {
        var time = 1L
        var name = "Old group"
        val resolver = InboxGroupNames { time }
        val transport = transport { _, _ -> json("""{"conversations":[{"id":"feed","name":"$name","is_group":true}]}""") }
        resolver.refresh(listOf(item()), transport)
        time += 300_001; name = "New group"
        resolver.refresh(listOf(item()), transport)
        assertEquals(name, resolver.name(item(), "hub"))
        try {
            resolver.refresh(listOf(item()), transport("other") { _, _ -> throw CancellationException() })
            fail("Cancellation must propagate")
        } catch (_: CancellationException) {
            assertNull(resolver.name(item(), "other"))
        }
    }

    @Test fun laterPageFailureKeepsVerifiedNamesWithoutInventingMissingOnes() = runBlocking {
        val resolver = InboxGroupNames()
        val missing = JsonObject(item() + ("lark" to json("""{"feed_id":"missing","is_group":true}""")))
        resolver.refresh(listOf(item(), missing), transport { _, cursor ->
            if (cursor != null) throw IllegalStateException("Page unavailable")
            json("""{"conversations":[{"id":"feed","name":"Verified group","is_group":true}],"next_cursor":"next"}""")
        })
        assertEquals("Verified group", resolver.name(item(), "hub"))
        assertNull(resolver.name(missing, "hub"))
    }

    @Test fun optionalLiveDtoUsesTheSameResolver() = runBlocking {
        val path = System.getenv("PERSONA_INBOX_LIVE_FIXTURE") ?: return@runBlocking
        val fixture = json(File(path).readText())
        val item = fixture.getValue("item").jsonObject
        val pages = fixture.getValue("pages").jsonArray.map { it.jsonObject }.iterator()
        val resolver = InboxGroupNames()
        resolver.refresh(listOf(item), transport { _, _ -> pages.next() })
        assertEquals(fixture.getValue("group_name").jsonPrimitive.content, resolver.name(item, "hub"))
        assertNotEquals(item.getValue("title").jsonPrimitive.content, resolver.name(item, "hub"))
    }
}
