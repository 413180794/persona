package persona.feature.inbox

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.MailTranslation
import persona.core.contracts.api.TranslationPrefs
import persona.core.contracts.api.TranslationTransport

@OptIn(ExperimentalCoroutinesApi::class)
class TranslatorTest {
    private class FakeHub : TranslationTransport {
        val batches = mutableListOf<Pair<List<String>, String>>()
        var mails = 0
        var fail = false
        override suspend fun translate(texts: List<String>, target: String): List<String> {
            batches += texts to target
            if (fail) error("offline")
            return texts.map { if (it == "?") "" else "【译】$it" }
        }
        override suspend fun translateMail(account: String, uid: Long, mailbox: String, target: String): MailTranslation {
            mails++
            return MailTranslation("主题", "正文")
        }
    }

    @Test fun textsAskedForWithinAMomentGoInBatches() = runTest {
        val hub = FakeHub()
        val t = Translator(MutableStateFlow(TranslationPrefs()), { hub }, backgroundScope, batch = 2)
        listOf("Hello there", "Good morning", "See you").forEach { t.request(it, "zh-Hans") }
        t.request("Hello there", "zh-Hans") // asked again while queued: once
        runCurrent()
        assertTrue(hub.batches.isEmpty())
        advanceTimeBy(301); runCurrent()
        assertEquals(listOf(listOf("Hello there", "Good morning"), listOf("See you")), hub.batches.map { it.first })
        assertEquals("【译】See you", t.translation("See you", "zh-Hans"))
        t.request("See you", "zh-Hans") // already have it
        advanceTimeBy(301); runCurrent()
        assertEquals(2, hub.batches.size)
    }

    @Test fun aFailedOrEmptyTranslationIsNotAskedAgainThisRun() = runTest {
        val hub = FakeHub()
        val t = Translator(MutableStateFlow(TranslationPrefs()), { hub }, backgroundScope)
        t.request("?", "en")
        advanceTimeBy(301); runCurrent()
        assertNull(t.translation("?", "en"))
        hub.fail = true
        t.request("Bonjour à tous", "zh-Hans")
        advanceTimeBy(301); runCurrent()
        hub.fail = false
        t.request("Bonjour à tous", "zh-Hans"); t.request("?", "en")
        advanceTimeBy(301); runCurrent()
        assertEquals(2, hub.batches.size)
    }

    @Test fun eachTargetIsItsOwnBatchAndNoHubAsksNothing() = runTest {
        val hub = FakeHub()
        var connected = false
        val t = Translator(MutableStateFlow(TranslationPrefs()), { hub.takeIf { connected } }, backgroundScope)
        t.request("Hello there", "zh-Hans")
        advanceTimeBy(301); runCurrent()
        assertTrue(hub.batches.isEmpty())
        connected = true
        t.request("Hello again", "zh-Hans"); t.request("Hello again", "ja")
        advanceTimeBy(301); runCurrent()
        assertEquals(setOf("zh-Hans", "ja"), hub.batches.map { it.second }.toSet())
    }

    @Test fun aMailIsAskedOncePerMessageAndTarget() = runTest {
        val hub = FakeHub()
        val t = Translator(MutableStateFlow(TranslationPrefs()), { hub }, backgroundScope)
        t.mail("a", 1, "INBOX", "zh-Hans"); t.mail("a", 1, "INBOX", "zh-Hans"); t.mail("a", 1, "INBOX", "en")
        assertEquals(2, hub.mails)
    }

    @Test fun foreignIsDecidedOnTheDevice() = runTest {
        val t = Translator(MutableStateFlow(TranslationPrefs()), { null }, backgroundScope)
        assertTrue(t.isForeign("Thanks for the update, please send the slides", "zh-Hans"))
        assertFalse(t.isForeign("好的，明天见", "zh-Hans"))
    }

    @Test fun withoutAHubATextIsAskedAgainLaterNotGivenUp() = runTest {
        val hub = FakeHub()
        var connected = false
        val t = Translator(MutableStateFlow(TranslationPrefs()), { hub.takeIf { connected } }, backgroundScope)
        t.request("Hello there", "zh-Hans")
        advanceTimeBy(301); runCurrent()
        connected = true
        t.request("Hello there", "zh-Hans")
        advanceTimeBy(301); runCurrent()
        assertEquals("【译】Hello there", t.translation("Hello there", "zh-Hans"))
    }

    @Test fun aTextQueuedRightAfterAFlushStillGoes() = runTest {
        val hub = FakeHub()
        val t = Translator(MutableStateFlow(TranslationPrefs()), { hub }, backgroundScope)
        t.request("First message here", "zh-Hans")
        advanceTimeBy(301); runCurrent()
        t.request("Second message here", "zh-Hans")
        advanceTimeBy(301); runCurrent()
        assertEquals("【译】Second message here", t.translation("Second message here", "zh-Hans"))
    }
}
