package persona.core.data

import android.content.Context
import android.content.ContextWrapper
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import persona.core.contracts.HubScope
import persona.core.contracts.Item
import persona.core.contracts.MeshHub
import persona.core.contracts.SessionKey
import persona.core.contracts.api.DraftKey
import persona.core.contracts.api.MessageRequest
import persona.core.contracts.api.MessageTransport
import persona.core.contracts.api.MessageTransportResult
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HubGroupsDataStoreTest {
    private val session = SessionKey("machine", "session")
    private val key = stringPreferencesKey("value")

    @Test fun aSinglePairingBecomesThePrimaryGroupAndGroupsOpenSideBySide() = runBlocking {
        fixture { context, parent ->
            var store = newStore(context, parent)
            try {
                // Before groups: one pairing behind active.enc, with cached items and list preferences.
                val single = store.pair(credentials("hub-a"))
                val oldScope = single.scope
                single.items.save(session, CachedChat(items = listOf(Item("old", role = "user", kind = "text", text = "cached"))))
                single.listPreferences.edit { it[key] = "list" }
                store.close()

                store = newStore(context, parent)
                val registry = store.registry()
                val primary = registry.groups.single()
                assertEquals(primary.id, registry.primary)
                assertEquals(StoredScope.of(oldScope), primary.storage)
                assertEquals(StoredScope.of(oldScope), primary.home.pairing)
                // restore() now waits for the selected group instead of opening a scope itself.
                val waiting = async(Dispatchers.IO) { store.restore() }
                delay(200)
                assertFalse(waiting.isCompleted)

                val a = store.openGroup(primary.storage.scope, primary.home.pairing.scope)
                assertEquals(oldScope, a.scope)
                assertEquals("old", a.items.load(session).items.single().id)
                assertEquals("list", a.listPreferences.data.first()[key])
                store.publish(a)
                assertSame(a, withTimeout(5000) { waiting.await() })
                assertSame(a, store.restore())
                // Opening an open group again returns it.
                assertSame(a, store.openGroup(primary.storage.scope, primary.home.pairing.scope))

                // A second hub of another group: its own pairing, its own data, open at the same time.
                val bScope = store.storeHub(credentials("hub-b"))
                val added = store.change { it.addPairedHub(StoredScope.of(bScope), "公司", 2) }
                assertNotEquals(primary.id, added.group)
                assertEquals(primary.id, added.document.primary)
                val b = store.openGroup(bScope, bScope)
                assertEquals("hub-b", b.credentials.hubId)
                b.items.save(session, CachedChat(items = listOf(Item("b-item", role = "user", kind = "text", text = "b"))))
                assertEquals("old", a.items.load(session).items.single().id)
                assertEquals("b-item", b.items.load(session).items.single().id)
                assertEquals("hub-b", store.credentials(bScope)!!.hubId)

                // b's group still owes its hub a message (no network yet) and has a draft.
                b.messages.send(session, "还没发出去", "op-merge")
                withTimeout(5000) { b.messages.outbox.first { list -> list.any { it.clientMessageId == "op-merge" } } }
                b.drafts.save(DraftKey.Chat(session), "写了一半", 0)

                // b's hello now lists a: the groups merge. b's data waits to move; nothing is erased yet.
                val merged = store.change { it.learn("hub-b", null, listOf(MeshHub("hub-a"), MeshHub("hub-b")), 3) }
                assertTrue(merged.erase.isEmpty())
                assertEquals(listOf(PendingMove(StoredScope.of(bScope), primary.id)), merged.document.moves)
                try { store.unsentAt(bScope); fail("An open group's data must not be read for a move") }
                catch (_: IllegalStateException) { }
                store.closeGroup(b)
                // The move: the message keeps its client_msg_id and joins a's outbox, the draft joins a's drafts.
                val unsent = store.unsentAt(bScope)
                assertEquals(1, unsent.messageCount)
                assertEquals(1, unsent.draftCount)
                assertEquals(2, a.adopt(unsent))
                val adopted = withTimeout(5000) { a.messages.outbox.first { list -> list.any { it.clientMessageId == "op-merge" } } }
                    .single { it.clientMessageId == "op-merge" }
                assertEquals("还没发出去", adopted.text)
                assertEquals(session, adopted.session)
                assertEquals("写了一半", a.drafts.read(DraftKey.Chat(session)).text)
                // Moving again after a crash adds nothing twice.
                assertEquals(0, a.adopt(store.unsentAt(bScope)))
                assertEquals(1, a.messages.outbox.value.count { it.clientMessageId == "op-merge" })
                // Then b's group data goes; its credentials stay.
                assertEquals(0, store.sweep())
                val moved = store.change { it.completeMove(StoredScope.of(bScope)) }
                assertEquals(listOf(ScopeErasure(StoredScope.of(bScope), Erase.Data)), moved.erase)
                store.erase(moved.erase)
                assertFalse(File(context.filesDir, "hub-data/${bScope.path}").exists())
                assertEquals("hub-b", store.credentials(bScope)!!.hubId)
                assertEquals(listOf("hub-a", "hub-b"), store.registry().groups.single().hubs.map { it.hubId })

                // Forgetting hub a keeps the group's data in a's directory; hub b becomes its home.
                val removed = store.change { it.removeHub(primary.id, "hub-a") }
                assertEquals(listOf(ScopeErasure(StoredScope.of(oldScope), Erase.Credentials)), removed.erase)
                store.closeGroup(a)
                assertNull(store.restore())
                store.erase(removed.erase)
                assertNull(store.credentials(oldScope))
                val group = removed.document.groups.single()
                val reopened = store.openGroup(group.storage.scope, group.home.pairing.scope)
                assertEquals(oldScope, reopened.scope)
                assertEquals("hub-b", reopened.credentials.hubId)
                assertEquals("old", reopened.items.load(session).items.single().id)
                assertEquals(0, store.sweep())

                // An orphan directory (a crash between writing the registry and erasing) is swept.
                File(context.filesDir, "hub-data/hub-x/orphan").mkdirs()
                assertEquals(1, store.sweep())
                assertFalse(File(context.filesDir, "hub-data/hub-x").exists())

                store.eraseAll()
                listOf(context.filesDir, context.cacheDir, context.noBackupFilesDir).forEach { root ->
                    assertTrue(scopeDirectories(listOf(root)).isEmpty())
                }
                assertEquals(GroupsDocument(), store.registry())
                assertNull(store.restore())
                store.close()

                // After a restart the empty registry stays empty: nothing is migrated again.
                store = newStore(context, parent)
                assertEquals(GroupsDocument(), store.registry())
            } finally { store.close() }
        }
    }

    private fun newStore(context: Context, parent: CoroutineScope) = HubDataStore(context, parent) { _, _ ->
        object : MessageTransport {
            override val readyToSend = MutableStateFlow(false)
            override val supportsClientOps = false
            override suspend fun send(request: MessageRequest): MessageTransportResult = error("Unexpected network call")
        }
    }

    private fun credentials(hub: String) = HubCredentials("https://$hub.example.com", hub, null, emptyList(), "token-$hub")

    private suspend fun fixture(test: suspend (Context, CoroutineScope) -> Unit) {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val root = File(base.cacheDir, "groups-test-${java.util.UUID.randomUUID()}").apply { mkdirs() }
        val context = object : ContextWrapper(base) {
            override fun getApplicationContext(): Context = this
            override fun getFilesDir() = File(root, "files").apply { mkdirs() }
            override fun getCacheDir() = File(root, "cache").apply { mkdirs() }
            override fun getNoBackupFilesDir() = File(root, "no-backup").apply { mkdirs() }
        }
        val parent = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        try { test(context, parent) }
        finally { parent.cancel(); root.deleteRecursively() }
    }
}
