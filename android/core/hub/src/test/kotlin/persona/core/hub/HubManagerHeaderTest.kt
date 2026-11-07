package persona.core.hub

import persona.core.contracts.api.AssistantManagerChoice
import java.net.InetAddress
import java.security.MessageDigest
import java.util.Base64
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The manager the assistant page shows names itself on the assistant's requests only (docs/assistant/managers.md step 4). */
class HubManagerHeaderTest {
    @After fun reset() { AssistantManagerChoice.reset(); AssistantManagerChoice.forgetItems() }

    @Test fun assistantRequestsNameTheChosenManager() {
        val certificate = HeldCertificate.Builder().commonName("example.com").build()
        val tls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        MockWebServer().use { server ->
            server.useHttps(tls.sslSocketFactory(), false)
            server.start(InetAddress.getLoopbackAddress(), 0)
            val pin = Base64.getUrlEncoder().withoutPadding()
                .encodeToString(MessageDigest.getInstance("SHA-256").digest(certificate.certificate.publicKey.encoded))
            val base = server.url("/").newBuilder().host(InetAddress.getLoopbackAddress().hostAddress!!).build()
            HubHttpClient("test-hub", pin, "test").use { hub ->
                fun header(path: String): String? {
                    server.enqueue(MockResponse().setBody("{}"))
                    hub.client.newCall(Request.Builder().url(base.resolve(path)!!).build()).execute().close()
                    return server.takeRequest().getHeader(ManagerHeader)
                }
                fun sent(path: String): Pair<String?, String?> {
                    server.enqueue(MockResponse().setBody("{}"))
                    hub.client.newCall(Request.Builder().url(base.resolve(path)!!).build()).execute().close()
                    return server.takeRequest().let { it.path to it.getHeader(ManagerHeader) }
                }
                // The default manager: requests as before.
                AssistantManagerChoice.choose("test-hub", null, "assistant")
                assertNull(header("/api/v1/assistant"))
                // Another manager: the assistant's requests name it, the rest go by ownership as before.
                AssistantManagerChoice.choose("test-hub", "m-2", "assistant")
                assertEquals("m-2", header("/api/v1/assistant"))
                // One conversation's routes are not the page's: its manager when a page listed it, else none (the hub
                // routes it by ownership).
                assertNull(header("/api/v1/assistant/conversations/c1/feed?after=3"))
                AssistantManagerChoice.noteItem(listOf("assistant", "conversations", "c1"), "m-2")
                AssistantManagerChoice.noteItem(listOf("assistant", "conversations", "h1"), "assistant")
                assertEquals("m-2", header("/api/v1/assistant/conversations/c1/feed?after=3"))
                assertNull(header("/api/v1/assistant/conversations/h1/stop"))
                assertEquals("m-2", header("/api/v1/assistant/conversations"))
                assertEquals("m-2", header("/hub/api/v1/assistant/memory"))
                assertNull(header("/api/v1/sessions/lab/s1/send"))
                assertNull(header("/api/v1/assistants"))
                // Picking the default again sends none; another hub's choice is not this one's.
                AssistantManagerChoice.choose("test-hub", "assistant", "assistant")
                assertNull(header("/api/v1/assistant"))
                AssistantManagerChoice.choose("other-hub", "m-2", "assistant")
                assertNull(header("/api/v1/assistant"))
                // A request pinned to a manager (`?manager=`) keeps it whatever the page shows; the default's (empty)
                // goes without the query or a header.
                AssistantManagerChoice.choose("test-hub", "m-2", "assistant")
                assertEquals("/api/v1/assistant?manager=m-3" to null, sent("/api/v1/assistant?manager=m-3"))
                assertEquals("/api/v1/assistant/conversations" to null, sent("/api/v1/assistant/conversations?manager="))
                // Another manager's things in a merged list: their own requests name it, whatever the page shows.
                AssistantManagerChoice.choose("test-hub", null, "assistant")
                AssistantManagerChoice.noteItem(listOf("inbox", "mail:a1:t:7"), "m-3")
                AssistantManagerChoice.noteItem(listOf("inbox", "lark:f1:2"), null)
                AssistantManagerChoice.noteItem(listOf("extensions", "notes"), "m-3")
                assertEquals("m-3", header("/api/v1/inbox/mail:a1:t:7"))
                assertEquals("m-3", header("/api/v1/inbox/mail:a1:t:7/done"))
                assertEquals("m-3", header("/hub/api/v1/extensions/notes/-/config"))
                assertNull(header("/api/v1/inbox/lark:f1:2/reply"))
                assertNull(header("/api/v1/inbox"))
                assertNull(header("/api/v1/inbox/done"))
                assertNull(header("/api/v1/extensions/other/-/"))
                // The default's own (or stamped with the default's id) need nothing.
                AssistantManagerChoice.noteItem(listOf("inbox", "mail:a2:t:1"), "assistant")
                assertNull(header("/api/v1/inbox/mail:a2:t:1"))
                // Stamped again without a manager: the default's now.
                AssistantManagerChoice.noteItem(listOf("extensions", "notes"), null)
                assertNull(header("/api/v1/extensions/notes/-/config"))
            }
        }
    }

    @Test fun assistantPaths() {
        assertTrue(isAssistantPath("https://example.com/api/v1/assistant".toHttpUrl()))
        assertTrue(isAssistantPath("https://example.com/persona/api/v1/assistant/tasks/1/run".toHttpUrl()))
        assertFalse(isAssistantPath("https://example.com/api/v1/inbox".toHttpUrl()))
        assertFalse(isAssistantPath("https://example.com/api/v1".toHttpUrl()))
        assertFalse(isAssistantPath("https://example.com/api/v1/assistant?space=health".toHttpUrl()))
        assertFalse(isAssistantPath("https://example.com/api/v1/assistant/conversations/c1/feed".toHttpUrl()))
        assertTrue(isAssistantPath("https://example.com/api/v1/assistant/conversations".toHttpUrl()))
    }

    /**
     * The same extension under two managers (each one's built-ins): the list notes neither, and an extension's page pins
     * its own manager while it is open, so the default's voice keeps going to the default elsewhere.
     */
    @Test fun anExtensionsPageNamesItsOwnManager() = kotlinx.coroutines.runBlocking {
        val api = HttpExtensionManagement({ _, _, _ ->
            okhttp3.Response.Builder().request(Request.Builder().url("https://example.com/").build()).protocol(okhttp3.Protocol.HTTP_1_1)
                .code(200).message("OK").body(run {
                    """{"extensions":[{"id":"voice","manager":"assistant"},{"id":"voice","manager":"m-3"},{"id":"notes","manager":"m-3"}]}""".toResponseBody()
                }).build()
        })
        AssistantManagerChoice.choose("hub", null, "assistant")
        assertEquals(listOf("assistant", "m-3", "m-3"), api.listing().extensions.map { it.manager })
        assertNull(AssistantManagerChoice.itemHeader(listOf("extensions", "voice", "-", "config")))
        val release = AssistantManagerChoice.pin(listOf(listOf("extensions", "voice"), listOf("plugins", "voice")), "m-3")
        assertEquals("m-3", AssistantManagerChoice.itemHeader(listOf("extensions", "voice", "-", "config")))
        assertNull(AssistantManagerChoice.itemHeader(listOf("extensions", "notes")))
        // the latest page wins, and each goes with its page
        val inner = AssistantManagerChoice.pin(listOf(listOf("extensions", "voice")), "assistant")
        assertNull(AssistantManagerChoice.itemHeader(listOf("extensions", "voice")))
        inner()
        assertEquals("m-3", AssistantManagerChoice.itemHeader(listOf("extensions", "voice")))
        release()
        assertNull(AssistantManagerChoice.itemHeader(listOf("extensions", "voice", "-", "config")))
    }

    /** What a hub's lists note before its roster is read is under that hub, and found once the roster lands. */
    @Test fun notesBeforeTheRosterAreTheHubs() {
        AssistantManagerChoice.atHub("hub-a")
        AssistantManagerChoice.noteItem(listOf("inbox", "x"), "m-1")
        val release = AssistantManagerChoice.pin(listOf(listOf("extensions", "voice")), "m-3")
        AssistantManagerChoice.choose("hub-a", null, "assistant")
        assertEquals("m-1", AssistantManagerChoice.itemHeader(listOf("inbox", "x", "done"), "hub-a"))
        assertEquals("m-3", AssistantManagerChoice.itemHeader(listOf("extensions", "voice"), "hub-a"))
        release()
    }

    /** What one hub's lists showed is that hub's: another paired hub's requests name nothing by it. */
    @Test fun stampsAreTheirHubs() {
        AssistantManagerChoice.choose("hub-a", null, "assistant")
        AssistantManagerChoice.noteItem(listOf("inbox", "x"), "m-1")
        assertEquals("m-1", AssistantManagerChoice.itemHeader(listOf("inbox", "x", "done"), "hub-a"))
        assertNull(AssistantManagerChoice.itemHeader(listOf("inbox", "x", "done"), "hub-b"))
        // the roster moved the default after the list was read: compared when the request goes out
        AssistantManagerChoice.choose("hub-a", null, "m-1")
        assertNull(AssistantManagerChoice.itemHeader(listOf("inbox", "x"), "hub-a"))
    }
}
