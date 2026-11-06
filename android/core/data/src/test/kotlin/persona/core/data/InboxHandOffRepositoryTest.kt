package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.feature.inbox.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import java.nio.file.Files

/** The data side of "AI draft", "Hand to AI" and the row labels. */
@OptIn(ExperimentalCoroutinesApi::class)
class InboxHandOffRepositoryTest {
    @Test fun recordsCarryLabelsMailAndWhoWroteTheDraft() = runTest {
        val directory = Files.createTempDirectory("inbox-labels-test").toFile()
        try {
            val transport = ItemsTransport(buildJsonArray {
                add(buildJsonObject {
                    put("id", "lark:f2:5"); put("source", "lark"); put("kind", "mention"); put("title", "项目群")
                    put("lark", buildJsonObject { put("feed_id", "f2"); put("at_all", true) })
                })
                add(buildJsonObject {
                    put("id", "mail:a1:t:7"); put("source", "mail"); put("kind", "mail"); put("title", "周报"); put("account_name", "me@example.com")
                    // The hub leaves direct out when the user was only copied.
                    put("mail", buildJsonObject { put("account_id", "a1"); put("mailbox", "INBOX"); put("uids", buildJsonArray { add(5); add(7) }) })
                })
                add(buildJsonObject {
                    put("id", "mail:a1:t:9"); put("source", "mail"); put("kind", "mail"); put("title", "促销")
                    put("mail", buildJsonObject { put("account_id", "a1"); put("uids", buildJsonArray { add(9) }); put("direct", true) })
                })
            })
            val machines = MutableStateFlow(listOf(Machine("laptop", name = "我的笔记本")))
            val repository = repository(transport, directory, backgroundScope, machines = machines)
            // The pairing's own start (which clears the list for it) comes first.
            repository.snapshot.first { it.contentSource != null }
            repository.refresh()
            val records = repository.snapshot.value.records.associateBy { it.item.id }
            assertTrue(records.getValue("lark:f2:5").atAll)
            assertEquals("lark", records.getValue("lark:f2:5").source)
            assertTrue(records.getValue("lark:f2:5").isLark)
            val copied = records.getValue("mail:a1:t:7").mail!!
            assertEquals(InboxMailRef("a1", "INBOX", listOf(5L, 7L), direct = false), copied)
            assertFalse(records.getValue("mail:a1:t:7").isLark)
            assertTrue(records.getValue("mail:a1:t:9").mail!!.direct)
            assertEquals("INBOX", records.getValue("mail:a1:t:9").mail!!.mailbox)
            repository.snapshot.first { it.machines["laptop"] == "我的笔记本" }

            // A draft AI wrote shows as its card; the user's own reads "Draft · …".
            repository.saveDraft(ReplyTarget.Inbox("mail:a1:t:7"), ReplyDraft("AI 写的", ai = true))
            repository.saveDraft(ReplyTarget.Inbox("mail:a1:t:9"), ReplyDraft("自己写的"))
            val drafted = repository.snapshot.value.records.associateBy { it.item.id }
            assertTrue(drafted.getValue("mail:a1:t:7").aiDraft)
            assertNull(drafted.getValue("mail:a1:t:7").localStatus)
            assertFalse(drafted.getValue("mail:a1:t:9").aiDraft)
            assertEquals("草稿 · 自己写的", drafted.getValue("mail:a1:t:9").localStatus)
            assertTrue(repository.readDraft(ReplyTarget.Inbox("mail:a1:t:7"))!!.ai)
            assertFalse(repository.readDraft(ReplyTarget.Inbox("mail:a1:t:9"))!!.ai)
        } finally {
            directory.deleteRecursively()
        }
    }

    // #1773: the list carries how triage stands, refreshed with it; an older hub says nothing, and the banner stays away.
    @Test fun refreshCarriesTheHubsTriage() = runTest {
        val directory = Files.createTempDirectory("inbox-triage-test").toFile()
        try {
            val transport = ItemsTransport(buildJsonArray {})
            val repository = repository(transport, directory, backgroundScope)
            repository.snapshot.first { it.contentSource != null }
            repository.refresh()
            assertNull(repository.snapshot.value.triage)

            transport.triage = buildJsonObject {
                put("state", "unconfigured"); put("waiting", 3)
                put("sources", buildJsonArray { add(buildJsonObject {
                    put("source", "lark"); put("name", "飞书"); put("state", "unconfigured"); put("waiting", 3)
                }) })
            }
            repository.refresh()
            assertEquals(InboxTriage("unconfigured", null, 3, listOf(InboxTriageSource("lark", "飞书", "unconfigured", waiting = 3))),
                repository.snapshot.value.triage)

            // Sorted again: back to ok, and the banner goes.
            transport.triage = buildJsonObject { put("state", "ok"); put("waiting", 0) }
            repository.refresh()
            assertEquals("ok", repository.snapshot.value.triage?.state)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test fun recordsCarryReadStateWithoutChangingMentionOrTier() = runTest {
        val directory = Files.createTempDirectory("inbox-read-test").toFile()
        try {
            val transport = ItemsTransport(buildJsonArray {
                for (read in listOf(false, true)) for (atAll in listOf(false, true)) {
                    add(buildJsonObject {
                        put("id", "mention-$read-$atAll"); put("source", "lark"); put("kind", "mention")
                        put("tier", "fyi"); put("read", read)
                        put("lark", buildJsonObject { put("feed_id", "feed"); put("at_all", atAll) })
                    })
                }
                add(buildJsonObject { put("id", "without-read"); put("kind", "mention") })
            })
            val repository = repository(transport, directory, backgroundScope)
            repository.snapshot.first { it.contentSource != null }
            repository.refresh()
            val records = repository.snapshot.value.records.associateBy { it.item.id }
            for (read in listOf(false, true)) for (atAll in listOf(false, true)) {
                val record = records.getValue("mention-$read-$atAll")
                assertEquals(read, record.read)
                assertEquals(atAll, record.atAll)
                assertEquals("mention", record.item.kind)
                assertEquals("fyi", record.item.tier)
            }
            assertFalse(records.getValue("without-read").read)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test fun handOffGoesToTheMainConversationOrTheAgentsOutboxAndDraftsAskTheHub() = runTest {
        val directory = Files.createTempDirectory("inbox-handoff-test").toFile()
        try {
            val main = RecordingMain()
            val agents = RecordingSender()
            val instructions = mutableListOf<Pair<String, String>>()
            val actions = object : InboxActionsTransport {
                override suspend fun collectNow() = Unit
                override suspend fun context(itemId: String) = "上下文"
                override suspend fun draft(itemId: String, instruction: String): String {
                    instructions += itemId to instruction
                    return "能赶上，周五前给你。"
                }
            }
            val repository = repository(ItemsTransport(JsonArray(emptyList())), directory, backgroundScope, main = main, agents = agents, actions = actions)
            repository.handOff(InboxHandOffRequest("item", "<jarvis-context …>\n\n总结一下", operationId = "op-main"))
            repository.handOff(InboxHandOffRequest("item", "交给 Agent", "laptop", "s1", "op-agent"))
            assertEquals(listOf("op-main" to "<jarvis-context …>\n\n总结一下"), main.sent)
            assertEquals(listOf(Triple(SessionKey("laptop", "s1"), "交给 Agent", "op-agent")), agents.sent)
            assertEquals("能赶上，周五前给你。", repository.draftReply("item", "在这版上改：能"))
            assertEquals(listOf("item" to "在这版上改：能"), instructions)

            // Without the services a hand-off says so instead of pretending.
            val bare = repository(ItemsTransport(JsonArray(emptyList())), directory.resolve("bare"), backgroundScope)
            assertTrue(runCatching { bare.handOff(InboxHandOffRequest("item", "x", "laptop", "s1")) }.exceptionOrNull() is IllegalStateException)
            assertTrue(runCatching { bare.draftReply("item", "") }.exceptionOrNull() is IllegalStateException)
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun repository(
        transport: ItemsTransport, directory: java.io.File, owner: CoroutineScope,
        machines: MutableStateFlow<List<Machine>> = MutableStateFlow(emptyList()),
        main: MainConversationSender? = null, agents: MessageSender? = null, actions: InboxActionsTransport? = null,
    ) = RealInboxRepository(
        object : InboxAssistantConnection { override val current = MutableStateFlow<InboxAssistantTransport?>(transport) },
        InboxReplyJournal(directory, "hub/generation"),
        object : SessionsRepository {
            override val machines = machines
            override val sessions = MutableStateFlow<List<Session>>(emptyList())
            override suspend fun refresh() = Unit
        },
        object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }, owner, mainConversationSender = main, actions = actions, messageSender = agents,
    ) { _, _ -> error("unused") }

    private class RecordingMain : MainConversationSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val sent = mutableListOf<Pair<String, String>>()
        override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) { sent += clientMessageId to text }
    }

    private class RecordingSender : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val sent = mutableListOf<Triple<SessionKey, String, String>>()
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            sent += Triple(session, text, clientMessageId)
        }
    }

    private class ItemsTransport(private val items: JsonArray, override val scope: String = "hub/generation") : InboxAssistantTransport {
        /** What the hub says of triage with the list (#1773); none: an older hub. */
        var triage: JsonObject? = null
        override suspend fun inbox() = buildJsonObject { put("items", items); triage?.let { put("triage", it) } }
        override suspend fun setDone(itemId: String, done: Boolean) = JsonObject(emptyMap())
        override suspend fun reply(itemId: String?, feedId: String?, text: String, clientId: String) = JsonObject(emptyMap())
        override suspend fun messages(feedId: String, around: Long?, before: String?, after: String?) = buildJsonObject {
            put("messages", JsonArray(emptyList())); put("sync_state", "complete")
        }
        override suspend fun assistant() = JsonObject(emptyMap())
        override suspend fun createConversation(main: Boolean) = JsonObject(emptyMap())
        override suspend fun pins() = JsonObject(emptyMap())
        override suspend fun mutes() = buildJsonObject { put("spaces", JsonArray(emptyList())); put("sessions", JsonArray(emptyList())) }
        override suspend fun media(ref: String) = byteArrayOf()
    }
}
