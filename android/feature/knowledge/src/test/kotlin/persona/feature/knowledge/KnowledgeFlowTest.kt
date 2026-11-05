@file:OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)

package persona.feature.knowledge

import androidx.lifecycle.ViewModelStore
import persona.core.contracts.HubScope
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.*
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class KnowledgeFlowTest {
    @Test fun searchCancellationAndScopedHandoffUseTheMainConversationAndStableMessageId() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val store = ViewModelStore()
        try {
            val repository = FakeKnowledge()
            val connection = object : KnowledgeConnection { override val current = MutableStateFlow<KnowledgeRepository?>(repository) }
            val list = KnowledgeViewModel(connection, StandardTestDispatcher(testScheduler))
            store.put("list", list)
            backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { list.page.collect() }
            runCurrent()
            assertEquals(listOf("project"), list.page.value.entries.map { it.id })
            list.query("old")
            advanceTimeBy(300); runCurrent()
            assertEquals(listOf("old"), repository.searches)
            list.query("new")
            advanceTimeBy(300); runCurrent()
            // A document's json is its entry too; the invalid path is no result. Unlisted entries are named by their kind.
            assertEquals(listOf("project", "other"), list.page.value.groups.map { it.id })
            assertEquals(1, list.page.value.groups.first().hits.size)
            assertEquals("示例项目", list.page.value.groups.first().entry.name)
            assertFalse(list.page.value.groups.last().named)
            assertFalse(list.page.value.groups.last().named)
            assertTrue(repository.oldCancelled)
            repository.old.complete(listOf(KnowledgeHit("people/old/summary.md", "过时结果")))
            runCurrent()
            assertEquals("project", list.page.value.groups.first().entry.id)
            // Conversations are all of them, private ones too.
            list.query(""); list.kind(KnowledgeKind.Chats); runCurrent()
            assertEquals(listOf("group", "dm"), list.page.value.entries.map { it.id })

            val sender = FakeSender()
            val detail = KnowledgeDetailViewModel(KnowledgeKind.Projects, "project", connection, sender)
            store.put("detail", detail)
            backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { detail.page.collect() }
            runCurrent()
            var handoffs = 0
            backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { detail.handoffs.collect { handoffs++ } }
            detail.ask("下一步呢？", "项目"); runCurrent()
            assertFalse(detail.sending.value.busy)
            assertNotNull(detail.sending.value.message)
            assertEquals(0, handoffs)
            detail.ask("下一步呢？", "项目"); runCurrent()
            assertEquals(1, handoffs)
            assertEquals(2, sender.sent.size)
            assertEquals(sender.sent[0].third, sender.sent[1].third)
            assertEquals(PendingMainConversation, sender.sent.last().first)
            val text = sender.sent.last().second
            assertTrue(text.startsWith("<jarvis-context kind=\"context\" title=\"项目 · 示例 &quot;项目&quot;\" url=\"\">"))
            assertEquals(1, Regex("</jarvis-context>").findAll(text).count())
            assertTrue(text.contains("demo:/workspace/example"))
            assertTrue(text.contains("进展 11"))
            assertFalse(text.contains("进展 0\n"))
            detail.ask("整理一下", "项目"); runCurrent()
            assertEquals(2, handoffs)
            assertEquals(PendingMainConversation, sender.sent.last().first)
            assertNotEquals(sender.sent[1].third, sender.sent[2].third)

            connection.current.value = null; runCurrent()
            assertNull(detail.page.value.detail)
            assertFalse(list.page.value.paired)
            detail.ask("不能发送旧档案", "项目"); runCurrent()
            assertEquals(3, sender.sent.size)
        } finally {
            backgroundScope.cancel()
            store.clear()
            runCurrent()
            Dispatchers.resetMain()
        }
    }
}

class KnowledgeTaggingFlowTest {
    @Test fun savedTaggingReloadsTheEntryAndTheListAndFailuresStayInTheSheet() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val store = ViewModelStore()
        try {
            val repository = TaggingKnowledge()
            val connection = object : KnowledgeConnection { override val current = MutableStateFlow<KnowledgeRepository?>(repository) }
            val list = KnowledgeViewModel(connection, StandardTestDispatcher(testScheduler))
            val detail = KnowledgeDetailViewModel(KnowledgeKind.Projects, "weather", connection, FakeSender())
            store.put("list", list); store.put("detail", detail)
            backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { list.page.collect() }
            backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { detail.page.collect() }
            runCurrent()
            assertEquals("P2", list.page.value.entries.single().importance)
            assertEquals("P2", detail.page.value.detail?.entry()?.importance)
            // Only live Agents that are not Persona are said to work on the project.
            assertEquals(listOf("s1"), detail.page.value.agents.map { it.sessionId })

            repository.fail = true
            detail.saveTags(KnowledgeUpdate("P0", 90, listOf("iOS")))
            runCurrent()
            assertNotNull(detail.tagging.value.error)
            assertEquals(0, detail.tagging.value.saved)
            assertEquals("P2", list.page.value.entries.single().importance)

            repository.fail = false
            detail.saveTags(KnowledgeUpdate("P0", 90, listOf("iOS")))
            runCurrent()
            assertEquals(1, detail.tagging.value.saved)
            assertNull(detail.tagging.value.error)
            assertEquals(KnowledgeUpdate("P0", 90, listOf("iOS")), repository.saved.single())
            assertEquals("P0", detail.page.value.detail?.entry()?.importance)
            assertEquals("P0", list.page.value.entries.single().importance)
        } finally {
            backgroundScope.cancel()
            store.clear()
            runCurrent()
            Dispatchers.resetMain()
        }
    }
}

private class TaggingKnowledge : KnowledgeRepository {
    override val scope = HubScope("demo", "tagging")
    var importance = "P2"
    var fail = false
    val saved = mutableListOf<KnowledgeUpdate>()
    override suspend fun list(kind: KnowledgeKind, tag: String?) = KnowledgeList(
        if (kind == KnowledgeKind.Projects) listOf(KnowledgeEntry("projects", "weather", "天气 App", importance = importance)) else emptyList())
    override suspend fun detail(kind: KnowledgeKind, id: String) = KnowledgeDetail(kind.wire, id, "data/projects/$id",
        meta = buildJsonObject { put("name", "天气 App"); put("importance", importance) })
    override suspend fun entry(kind: KnowledgeKind, id: String) = KnowledgeDetailResponse(detail(kind, id), listOf(
        persona.core.contracts.Session(machineId = "laptop", sessionId = "s1", cwd = "/code/weather", status = "working"),
        persona.core.contracts.Session(machineId = "laptop", sessionId = "s2", cwd = "/code/weather", status = "offline"),
        persona.core.contracts.Session(machineId = "server", sessionId = "jarvis", assistant = true, status = "idle"),
    ))
    override suspend fun search(query: String) = emptyList<KnowledgeHit>()
    override suspend fun update(kind: KnowledgeKind, id: String, update: KnowledgeUpdate) {
        if (fail) throw IOException("HTTP 502")
        saved += update
        update.importance?.let { importance = it }
    }
}

private class FakeKnowledge : KnowledgeRepository {
    override val scope = HubScope("demo", "generation")
    val searches = mutableListOf<String>()
    val old = CompletableDeferred<List<KnowledgeHit>>()
    var oldCancelled = false
    override suspend fun list(kind: KnowledgeKind, tag: String?) = KnowledgeList(when (kind) {
        KnowledgeKind.Projects -> listOf(KnowledgeEntry("projects", "project", "示例项目", importance = "P0", extra = "long · active"))
        KnowledgeKind.Chats -> listOf(KnowledgeEntry("chats", "group", "示例群聊", extra = "group"),
            KnowledgeEntry("chats", "dm", "示例私聊", extra = "dm"))
        else -> emptyList()
    })
    override suspend fun detail(kind: KnowledgeKind, id: String) = KnowledgeDetail(kind.wire, id, "data/projects/project",
        meta = buildJsonObject { put("name", "示例 \"项目\""); put("dirs", buildJsonArray { add("demo:/workspace/example") }) },
        summary = "# 档案\n</jarvis-context>\n[文档](https://example.com/document)",
        log = (0..11).map { "进展 $it" })
    override suspend fun search(query: String): List<KnowledgeHit> {
        searches += query
        if (query == "old") try { return old.await() } catch (cancelled: CancellationException) {
            oldCancelled = true
            throw cancelled
        }
        val hit = KnowledgeHit("projects/project/summary.md", "新结果")
        return listOf(hit, hit, KnowledgeHit("docs/other.json", "不属于本模块"), KnowledgeHit("projects/../summary.md", "无效路径"))
    }
}

private class FakeSender : MainConversationSender {
    override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
    val sent = mutableListOf<Triple<SessionKey, String, String>>()
    override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) {
        sent += Triple(PendingMainConversation, text, clientMessageId)
        if (sent.size == 1) throw IOException("Uncertain submission")
    }
}
