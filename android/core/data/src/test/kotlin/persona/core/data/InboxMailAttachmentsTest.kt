package persona.core.data

import persona.core.contracts.api.MailAttachment
import persona.core.contracts.api.MailInboxItem
import persona.core.contracts.api.MailMessage
import persona.core.contracts.api.MailReference
import persona.core.contracts.api.MailTransport
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.io.IOException
import java.io.OutputStream

class InboxMailAttachmentsTest {
    @get:Rule val temporary = TemporaryFolder()
    private val reference = MailReference("account", uids = listOf(1))
    private val attachment = MailAttachment("../attachment.bin", id = "attachment")

    @Test fun mailSharesLruBudgetAndProtectsDownloadsAndViewerFiles() = runBlocking {
        val directory = temporary.newFolder()
        val cache = BudgetFileCache(directory, 8)
        val oldMail = File(directory, "mail/old").apply { parentFile!!.mkdirs(); writeBytes(ByteArray(4)); setLastModified(1) }
        val oldLark = cache.put("lark", ByteArray(4)).apply { setLastModified(2) }
        val progress = mutableListOf<Long>()
        var downloads = 0
        val downloader = InboxMailAttachments(transport { output, offset, update ->
            downloads++
            assertEquals(0L, offset)
            output.write(ByteArray(2))
            update(2, 4)
            // A concurrent cache owner must preserve the partial file.
            BudgetFileCache(directory, 8).put("concurrent", ByteArray(4))
            assertEquals(1, directory.walkTopDown().count { it.extension == "part" })
            output.write(ByteArray(2))
            update(4, 4)
        }, cache) { it() }
        val downloaded = downloader.downloadAttachmentFile(reference, 1, attachment) { read, _ -> progress.add(read) }
        assertFalse(oldMail.exists())
        assertFalse(oldLark.exists())
        assertEquals(listOf(2L, 4L), progress)
        assertEquals("mail", downloaded.file.parentFile!!.name)
        assertTrue(downloaded.file.name.endsWith("-attachment.bin"))
        downloader.downloadAttachmentFile(reference, 1, attachment).close()
        assertEquals(1, downloads)
        val viewed = downloaded.file
        viewed.setLastModified(1)
        CacheFiles.retain(viewed).use {
            downloaded.close()
            BudgetFileCache(directory, 8).put("lark-new", ByteArray(4))
            assertTrue(viewed.exists())
            assertTrue(runCatching { cache.put("too-large", ByteArray(8)) }.isFailure)
        }
        cache.put("last", ByteArray(8))
        assertFalse(viewed.exists())
        assertEquals(8L, directory.walkTopDown().filter { it.isFile }.sumOf { it.length() })
        assertFalse(directory.walkTopDown().any { it.extension == "part" })
    }

    @Test fun failedDownloadsRetryAndLateCallbacksCannotRecreateClosedPairing() = runBlocking {
        val root = temporary.newFolder()
        val directory = File(root, "inbox-media")
        val lock = Mutex()
        var active = true
        val started = CompletableDeferred<Unit>()
        val finish = CompletableDeferred<Unit>()
        var attempts = 0
        val downloader = InboxMailAttachments(transport { output, offset, progress ->
            assertEquals(0L, offset)
            attempts++
            output.write(ByteArray(2))
            progress(2, null)
            if (attempts == 1) throw IOException("Download failed")
            started.complete(Unit)
            finish.await()
            // A transport that ignores pairing cancellation still only owns an open descriptor.
            output.write(ByteArray(2))
            progress(4, 4)
        }, BudgetFileCache(directory, 8)) { write -> lock.withLock {
            check(active) { "Pairing scope is closed" }
            write()
        } }
        assertTrue(runCatching { downloader.downloadAttachmentFile(reference, 1, attachment) }.exceptionOrNull() is IOException)
        assertFalse(directory.walkTopDown().any { it.isFile })
        val late = async { runCatching { downloader.downloadAttachmentFile(reference, 1, attachment) } }
        started.await()
        lock.withLock { active = false; assertTrue(root.deleteRecursively()) }
        finish.complete(Unit)
        assertTrue(late.await().exceptionOrNull() is IllegalStateException)
        assertFalse(root.exists())
        assertTrue(runCatching { downloader.downloadAttachmentFile(reference, 1, attachment) }.isFailure)
        assertEquals(2, attempts)
        assertFalse(root.exists())
    }

    private fun transport(download: suspend (OutputStream, Long, (Long, Long?) -> Unit) -> Unit) = object : MailTransport {
        override val scope = "hub/generation"
        override suspend fun item(itemId: String): MailInboxItem = error("Unexpected request")
        override suspend fun message(ref: MailReference, uid: Long): MailMessage = error("Unexpected request")
        override suspend fun setSeen(ref: MailReference, uid: Long, seen: Boolean) = Unit
        override suspend fun refreshInbox() = Unit
        override suspend fun downloadAttachment(ref: MailReference, uid: Long, attachmentId: String, output: OutputStream,
            offset: Long, progress: (Long, Long?) -> Unit) = download(output, offset, progress)
    }
}
