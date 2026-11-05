package persona.android.platform

import persona.core.contracts.JsonCodec
import persona.core.contracts.LockTestEvent
import persona.core.contracts.ServerEvent
import persona.core.contracts.SessionKey
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.io.IOException

class PlatformEntryTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun offlineSharesSurviveRestartAndResumeAttachmentsBeforeOneMainSend() = runTest {
        val directory = temporary.newFolder()
        val first = File(directory, "first.bin").apply { writeText("first attachment") }
        val second = File(directory, "second.bin").apply { writeText("second attachment") }
        val file = File(directory, "entry.json")
        val entry = QueuedShare("stable-id", "文件", "", listOf(
            SharedAttachment(first.name, "first.txt"), SharedAttachment(second.name, "second.txt")), queued = true)
        writePlatformFile(file, JsonCodec.encodeToString(QueuedShare.serializer(), entry))
        val uploads = mutableListOf<String>()
        val sent = mutableListOf<Pair<String, String>>()
        val main: suspend (String, String) -> Unit = { text, id -> sent += id to text }
        val agent: suspend (SessionKey, String, String) -> Unit = { _, _, _ -> error("Wrong destination") }
        val target = SessionKey("machine", "main-session")
        assertTrue(deliverQueuedShare(file, false, "Shared content", { error("Offline must not resolve main") },
            { _, _, _ -> error("Offline must not upload") }, main, agent))
        assertTrue(first.exists() && second.exists())
        try {
            deliverQueuedShare(file, true, "Shared content", { target }, { _, attachment, _ ->
                uploads += attachment.name
                if (attachment.name == second.name) throw IOException("Disconnected")
                "/tmp/shared/first.txt"
            }, main, agent)
            fail("Expected interrupted upload")
        } catch (_: IOException) { }
        assertTrue(sent.isEmpty())
        assertTrue(first.exists() && second.exists())
        val restored = JsonCodec.decodeFromString<QueuedShare>(file.readText())
        assertEquals("/tmp/shared/first.txt", restored.attachments.first().path)
        assertFalse(deliverQueuedShare(file, true, "Shared content", { target }, { _, attachment, _ ->
            uploads += attachment.name
            assertEquals("second attachment", attachment.readText())
            "/tmp/shared/second.txt"
        }, main, agent))
        assertEquals(listOf(first.name, second.name, second.name), uploads)
        assertEquals("stable-id", sent.single().first)
        assertTrue(sent.single().second.contains("'/tmp/shared/first.txt' '/tmp/shared/second.txt'"))
        assertFalse(directory.exists())

        val textFile = File(temporary.newFolder(), "entry.json")
        writePlatformFile(textFile, JsonCodec.encodeToString(QueuedShare.serializer(), QueuedShare("offline-text", "", "你好", queued = true)))
        assertFalse(deliverQueuedShare(textFile, false, "Shared content", { error("Main is resolved by core outbox later") },
            { _, _, _ -> error("No attachments") }, main, agent))
        assertEquals("offline-text", sent.last().first)
    }

    @Test fun lockEventsRequireIndependentFieldsAndReceiptPreservesUnknownDisplay() {
        val event = ServerEvent.decode("""{"type":"lock_test","lock_test_id":"test-id","lock_test_due_at":"2026-10-31T00:00:30Z","lock_test_sent_at":"2026-10-31T00:00:30Z"}""")
        assertTrue(event is LockTestEvent)
        assertEquals(event, ServerEvent.decode(JsonCodec.encodeToString(ServerEvent.serializer(), event!!)))
        assertNull(ServerEvent.decode("""{"type":"lock_test","id":"wrong-id"}"""))
        val sent = decodeLockTest(JsonCodec.parseToJsonElement("""{"test_id":"test-id","status":"sent","due_at":"2026-10-31T00:00:30Z"}""").jsonObject)
        assertNull(sent.receivedAt)
        assertNull(sent.notificationShown)
        val received = decodeLockTest(JsonCodec.parseToJsonElement("""{"test_id":"test-id","status":"received","due_at":"2026-10-31T00:00:30Z","received_at":"2026-10-31T00:00:31Z","notification_shown":false}""").jsonObject)
        assertEquals(false, received.notificationShown)
        assertNotNull(received.receivedAt)
        assertFalse(needsLocalNetworkPermission(36, 37))
        assertFalse(needsLocalNetworkPermission(37, 36))
        assertTrue(needsLocalNetworkPermission(37, 37))
    }
}
